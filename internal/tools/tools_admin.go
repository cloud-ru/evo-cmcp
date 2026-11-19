// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/cloud-ru/evo-cmcp/internal/auth"
	"github.com/cloud-ru/evo-cmcp/internal/engine"
	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/httpretry"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
	"github.com/cloud-ru/evo-cmcp/internal/pager"
)

// adminGET performs an authenticated GET against an absolute Cloud.ru API URL
// and returns the decoded JSON. Used for the read-only IAM / Organization
// admin endpoints (they share the same IAM bearer token).
func (s *Registry) adminGET(ctx context.Context, rawurl string) (map[string]any, error) {
	eng, err := s.engineFor(ctx, nil)
	if err != nil {
		return nil, err
	}
	return tokenGET(ctx, eng.Auth, rawurl)
}

// tokenGET is adminGET with the token of tp.
func tokenGET(ctx context.Context, tp auth.TokenProvider, rawurl string) (map[string]any, error) {
	tok, err := tp.Token(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok.Value)
	resp, err := OrgClient(30 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if e := json.Unmarshal(data, &m); e != nil {
		return nil, &adminHTTPError{Status: resp.StatusCode, Msg: string(data[:min(len(data), 300)])}
	}
	if resp.StatusCode != 200 {
		msg, _ := m["message"].(string)
		return nil, &adminHTTPError{Status: resp.StatusCode, Msg: msg}
	}
	return m, nil
}

// adminHTTPError is an answer of the API other than 200 (or not JSON): the
// API was reached and refused, unlike a network error.
type adminHTTPError struct {
	Status int
	Msg    string
}

func (e *adminHTTPError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Msg) }

// projectID returns the configured project id.
func (s *Registry) projectID(ctx context.Context) string {
	eng, err := s.newEngine(nil)
	if err != nil {
		return ""
	}
	return eng.Config.ProjectID
}

// handleIamServiceAccounts lists service accounts for a project (or the current
// one). This is where a k8s cluster_sa_id can be discovered.
func (s *Registry) handleIamServiceAccounts(ctx context.Context, args map[string]any) (string, bool) {
	pid := firstNonEmptyStr(stringArg(args, "project_id"), s.projectID(ctx))
	var raw []any
	err := pager.All("iam: service accounts", func(token string) (string, error) {
		u := "https://iam.api.cloud.ru/api/v2/serviceAccounts?target.projectId=" + neturl.QueryEscape(pid)
		if token != "" {
			u += "&pageToken=" + neturl.QueryEscape(token)
		}
		m, err := s.adminGET(ctx, u)
		if err != nil {
			return "", err
		}
		page, _ := m["service_accounts"].([]any)
		if page == nil {
			page, _ = m["serviceAccounts"].([]any)
		}
		raw = append(raw, page...)
		next, _ := m["next_page_token"].(string)
		if next == "" {
			next, _ = m["nextPageToken"].(string)
		}
		return next, nil
	})
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		sa, _ := x.(map[string]any)
		if sa == nil {
			continue
		}
		out = append(out, map[string]any{
			"id": sa["id"], "name": sa["name"], "email": sa["email"],
			"type": sa["type"], "enabled": sa["enabled"],
		})
	}
	return toJSON(map[string]any{"service_accounts": out}), false
}

// OrgProjectsURL is the project list of the Organization API (a variable
// for the tests of the CLI, whose 'evo project list' calls org_projects).
var OrgProjectsURL = "https://organization.api.cloud.ru/v3/projects"

func (s *Registry) handleOrgProjects(ctx context.Context, args map[string]any) (string, bool) {
	eng, err := s.engineFor(ctx, nil)
	if err != nil {
		return failed(ctx, err, nil), true
	}
	// Only a refusal of the first page says the key cannot list projects;
	// one of a later page is a failure of the call.
	raw, laterPage, err := orgProjectPages(ctx, eng.Auth)
	var he *adminHTTPError
	if errors.As(err, &he) && (laterPage || he.Status != http.StatusBadRequest && he.Status != http.StatusForbidden && he.Status != http.StatusNotFound) {
		return failed(ctx, orgHTTPError(he), nil), true
	}
	active := eng.Config.ProjectID
	if errors.As(err, &he) {
		hint := "Not an error: the key works in the active project (whoami). Pass project_id where a tool takes one."
		if active == "" {
			hint = orgNoProjectHint
		}
		return toJSON(map[string]any{
			"projects":    nil,
			"active":      active,
			"note":        ScopeText(0, false),
			"http_status": he.Status,
			"hint":        hint,
		}), false
	}
	if err != nil {
		return failed(ctx, err, nil), true
	}
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		p, _ := x.(map[string]any)
		// Bound to its project (SetProjectBound), the registry names no
		// other project of the organization.
		if p == nil || s.projectBound && p["id"] != active {
			continue
		}
		out = append(out, map[string]any{
			"id": p["id"], "name": p["name"], "status": p["status"],
			"organization_unit_id": p["organization_unit_id"],
		})
	}
	// active: the project commands work in, as 'evo project list' marks
	// it (spec REQ-4).
	return toJSON(map[string]any{"projects": out, "active": active}), false
}

func orgProjectPages(ctx context.Context, tp auth.TokenProvider) (raw []any, laterPage bool, err error) {
	// An empty page ends the list of the Organization API (IAM-10).
	err = pager.AllUntilEmpty("organization: projects", pager.MaxPages, func(token string) (string, int, error) {
		u := OrgProjectsURL
		if token != "" {
			u += "?page_token=" + neturl.QueryEscape(token)
		}
		m, err := tokenGET(ctx, tp, u)
		if err != nil {
			laterPage = token != ""
			return "", 0, err
		}
		page, _ := m["projects"].([]any)
		raw = append(raw, page...)
		next, _ := m["next_page_token"].(string)
		return next, len(page), nil
	})
	return raw, laterPage, err
}

// orgNoProjectHint is the hint of org_projects when the key cannot list
// projects and none is configured (docs/auth.md, «UUID проекта»).
const orgNoProjectHint = "No project is set, and this key cannot list projects (a service-account key never can). " +
	"Take the project UUID from the Cloud.ru console (the project where the service account has its roles) and set it: " +
	"evo configure --project-id <uuid>, CLOUDRU_PROJECT_ID, or project_id where a tool takes one."

// orgHTTPError gives an answer of the Organization API its reason: 401 —
// AUTH_FAILED, 429 — API_RATE_LIMITED, the rest — API_ERROR (not
// NOT_FOUND from a "not found" in the body).
func orgHTTPError(he *adminHTTPError) error {
	code, hint := clierrors.CodeAPIError, "Cloud.ru Organization API failed; retry later. The key still works in the active project (whoami)."
	switch {
	case he.Status == http.StatusUnauthorized:
		code, hint = clierrors.CodeAuthFailed, "Check the key (evo whoami); re-create it in the Cloud.ru console if it was revoked."
	case he.Status == http.StatusTooManyRequests:
		code, hint = clierrors.CodeAPIRateLimited, "Too many requests; retry in a minute."
	}
	return clierrors.Wrap(code, "Organization API: "+he.Error(), hint, he)
}

// quotaUsagesURL is the quota usages of the organization API, of the
// project when projectID is set.
func quotaUsagesURL(projectID string) string {
	url := "https://organization.api.cloud.ru/v3/quotas/usages?page_size=200"
	if projectID != "" {
		url += "&subject_type_codes=QUOTA_SUBJECT_TYPE_PROJECT&subject_ids=" + neturl.QueryEscape(projectID)
	}
	return url
}

func QuotaProbe(ctx context.Context, tp auth.TokenProvider, projectID string) error {
	_, err := tokenGET(ctx, tp, quotaUsagesURL(projectID))
	var he *adminHTTPError
	if errors.As(err, &he) && he.Status == http.StatusForbidden {
		return fmt.Errorf("%w: %s", engine.ErrQuotasForbidden, he.Msg)
	}
	return err
}

// handleOrgQuotas lists quota usages (limits vs usage) — useful for capacity
// planning. Requires the caller to have customer/project quota permissions.
func (s *Registry) handleOrgQuotas(ctx context.Context, args map[string]any) (string, bool) {
	url := quotaUsagesURL(stringArg(args, "project_id"))
	var raw []any
	err := pager.All("organization: quota usages", func(token string) (string, error) {
		u := url
		if token != "" {
			u += "&page_token=" + neturl.QueryEscape(token)
		}
		m, err := s.adminGET(ctx, u)
		if err != nil {
			return "", err
		}
		page, _ := m["usages"].([]any)
		raw = append(raw, page...)
		next, _ := m["next_page_token"].(string)
		return next, nil
	})
	if err != nil {
		// The org quota endpoint needs a customer/organization scope a service
		// account typically lacks (403 "dont have permissions to customers").
		// That's an expected limitation, not a failure — degrade to a soft
		// "unavailable" so it doesn't read as a hard error on an early turn.
		low := strings.ToLower(err.Error())
		if strings.Contains(low, "403") || strings.Contains(low, "permission") {
			return toJSON(map[string]any{
				"status":       "unavailable",
				"quota_usages": []any{},
				"reason":       clierrors.CodeAPIError,
				"note": i18n.T("this service account lacks customer/organization quota scope; quota usages are not readable with the current credentials",
					"у сервисного аккаунта нет прав на квоты организации: использование квот этим ключом не прочитать"),
				"detail": err.Error(),
			}), false
		}
		return errResult(err), true
	}
	types := s.quotaTypes(ctx)
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		u, _ := x.(map[string]any)
		if u == nil {
			continue
		}
		row := map[string]any{
			"resource": u["resource_type_code"], "limit": u["individual_limit"], "usage": u["individual_usage"],
		}
		code, _ := u["resource_type_code"].(string)
		for k, v := range types[code] {
			row[k] = v
		}
		out = append(out, row)
	}
	return toJSON(map[string]any{"quota_usages": out}), false
}

func (s *Registry) quotaTypes(ctx context.Context) map[string]map[string]any {
	m, err := s.adminGET(ctx, "https://organization.api.cloud.ru/v3/quotas/resourceTypes")
	if err != nil {
		return nil
	}
	items, _ := m["resource_types"].([]any)
	out := map[string]map[string]any{}
	for _, x := range items {
		t, _ := x.(map[string]any)
		code, _ := t["code"].(string)
		if code == "" {
			continue
		}
		named := map[string]any{}
		for field, key := range map[string]string{"name": "name", "unit": "unit", "product_name": "product"} {
			if v, _ := t[field].(string); v != "" {
				named[key] = v
			}
		}
		out[code] = named
	}
	return out
}

func OrgClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &httpretry.Transport{Policy: httpretry.Policy{Keep404: true}}}
}
