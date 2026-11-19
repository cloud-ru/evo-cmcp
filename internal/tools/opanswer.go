// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cloud-ru/evo-cmcp/internal/clock"
	"github.com/cloud-ru/evo-cmcp/internal/engine"
	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/httpretry"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
)

type resourceRead struct{ tool, arg, noState, noStateRU string }

var operationReads = map[string][]resourceRead{
	"ar_operation":    {{"ar_get", "id", "", ""}},
	"cert_operation":  {{"cert_get", "id", "the state of a certificate is the object issueState, not a word of the answer", "состояние сертификата — объект issueState, а не слово ответа"}},
	"dns_operation":   {{"dns_zone_get", "id", "", ""}, {"dns_record_get", "record_id", "a DNS record has no state in the DNS specification", "у записи DNS в спецификации DNS нет состояния"}},
	"k8s_operation":   {{"k8s_get", "cluster_id", "", ""}, {"k8s_nodepool_get", "nodepool_id", "", ""}},
	"kafka_operation": {{"kafka_get", "cluster_id", "", ""}},
	"lb_operation":    {{"lb_get", "id", "", ""}, {"tg_get", "id", "", ""}},
	"pg_operation":    {{"pg_get", "id", "", ""}},
	"redis_operation": {{"redis_get", "cluster_id", "", ""}},
	"vpc_operation":   {{"vpc_get", "id", "a VPC has no state in the VPC specification (only its static routes do)", "у сети VPC в спецификации VPC нет состояния (оно есть только у статических маршрутов)"}},
}

// operation answers a *_operation call: get reads the operation named by
// operation_id, wait_seconds waits for it to end (waitForOpJSON).
func (s *Registry) operation(ctx context.Context, tool string, args map[string]any, get func(context.Context) (any, error)) (string, bool) {
	fetch := func(ctx context.Context) (map[string]any, error) {
		op, err := get(ctx)
		if err != nil {
			return nil, err
		}
		m := opToMap(op)
		for _, k := range decls[tool].Untrusted.own {
			delete(m, k)
		}
		return m, nil
	}
	var m map[string]any
	var err error
	if w := pgInt(args, "wait_seconds", 0); w > 0 {
		m, err = waitForOpJSON(ctx, w, fetch)
	} else {
		m, err = fetch(ctx)
	}
	if err != nil {
		return errResult(err), true
	}
	addElapsed(m)
	s.addResourceStatus(ctx, tool, m)
	quotaFailure(tool, m)
	return toJSON(m), false
}

func quotaFailure(tool string, m map[string]any) {
	if done, _ := m["done"].(bool); !done {
		return
	}
	msg, code, failed := opFailure(m["error"])
	text, _ := msg.(string)
	if !failed || !isQuotaForm(text) {
		return
	}
	details := map[string]any{"cause": "quota"}
	if c := operationCode(code); c != nil {
		details["platform_code"] = c
	}
	m["details"] = details
	hint := quotaHint(true)
	if m["resource_status"] == "FAILED" {
		if del := failedDeletes[tool]; del != "" {
			hint += " " + i18n.Tf("The resource is left FAILED. What to do with it is the person's decision: %s removes it after their approval; a database cluster in FAILED may not be deletable through the API — then the Cloud.ru console or support.",
				"Ресурс остался в FAILED. Что с ним делать — решает человек: %s удаляет его после его согласия; кластер базы в FAILED через API может не удалиться — тогда консоль Cloud.ru или поддержка.", del)
		} else {
			hint += " " + i18n.T("The resource is left FAILED. What to do with it is the person's decision.",
				"Ресурс остался в FAILED. Что с ним делать — решает человек.")
		}
	}
	m["hint"] = hint
}

var quotaForm = regexp.MustCompile(`(?i)(^|[^a-z0-9_-])(acquire_quota|[a-z0-9_]{0,50}quota_exceeded|quota exceeded|quotaexceeded)([^a-z0-9_-]|$)`)

var quotedPart = regexp.MustCompile("\"[^\"]*\"|'[^']*'|`[^`]*`|«[^»]*»")

var fileSystemQuota = regexp.MustCompile(`(?i)\bdisk quota exceeded\b`)

// isQuotaForm reports a form of quotaForm in text outside its parts in
// quotes and the quota of a file system.
func isQuotaForm(text string) bool {
	text = quotedPart.ReplaceAllString(text, " ")
	text = fileSystemQuota.ReplaceAllString(text, " ")
	return quotaForm.MatchString(text)
}

// failedDeletes are the deletes of the resource of an operation that is
// left FAILED, named in the hint: the database clusters, whose create a
// quota fails.
var failedDeletes = map[string]string{
	"pg_operation":    "pg_delete",
	"kafka_operation": "kafka_delete",
	"redis_operation": "redis_delete",
}

func operationCode(code any) any {
	switch c := code.(type) {
	case float64:
		return c
	case string:
		if operationToken.MatchString(c) {
			return c
		}
	}
	return nil
}

var operationToken = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

func addElapsed(m map[string]any) {
	created, ok := operationTime(m, "created_at", "createdAt")
	if !ok {
		m["elapsed_note"] = i18n.T("the operation carries no time of its creation", "операция не несёт времени создания")
		return
	}
	end := clock.Now()
	if done, _ := m["done"].(bool); done {
		updated, ok := operationTime(m, "updated_at", "updatedAt")
		if !ok {
			m["elapsed_note"] = i18n.T("the operation is done and carries no time of its end", "операция завершена и не несёт времени окончания")
			return
		}
		end = updated
	}
	sec := 0
	if !end.Before(created) {
		sec = int(end.Sub(created) / time.Second)
	}
	m["elapsed_sec"] = sec
}

func operationTime(m map[string]any, keys ...string) (time.Time, bool) {
	for _, k := range keys {
		if v, _ := m[k].(string); v != "" {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

func (s *Registry) goingOn(ctx context.Context, ans map[string]any, opTool, opID, id string, listed func(context.Context) (map[string]any, error)) {
	if opID == "" {
		return
	}
	nextStep(ans, opTool, map[string]any{"operation_id": opID, "wait_seconds": opWaitMaxSeconds})
	ctx, cancel := context.WithTimeout(httpretry.Once(ctx), goingOnLimit)
	defer cancel()
	m := map[string]any{"resource_id": id}
	switch {
	case listed != nil:
		item, err := listed(ctx)
		switch {
		case err != nil:
			m["resource_status_error"] = clierrors.Reason(err)
		case item == nil:
			m["resource_status"] = "not_found"
			m["resource_status_note"] = notFoundYet()
		default:
			for _, k := range []string{"status", "state", "phase"} {
				if v, _ := item[k].(string); v != "" {
					m["resource_status"] = v
					cloudText(m, "resource_status")
					break
				}
			}
		}
	case id != "":
		s.addResourceStatus(ctx, opTool, m)
		switch note, _ := m["resource_status_note"].(string); {
		case m["resource_status"] == "not_found":
			m["resource_status_note"] = notFoundYet()
		case note != "":
			// A create's answer: the step is the read, not a repeat.
			m["resource_status_note"] = note + i18n.T(" — do not create it again", " — не создавайте его повторно")
		}
	}
	if m["resource_status"] == nil && m["resource_status_error"] == nil && m["resource_status_note"] == nil {
		m["resource_status_error"] = clierrors.CodeAPIError
		m["resource_status_note"] = noStateRead(id)
	}
	for _, k := range []string{"resource_status", "resource_status_error", "resource_status_note"} {
		if v, ok := m[k]; ok {
			ans[k] = v
		}
	}
	if m["_untrusted"] != nil {
		cloudText(ans, "resource_status")
	}
}

// goingOnLimit bounds the read of the state goingOn adds to the answer of
// a create: the limit of a preview's read.
var goingOnLimit = engine.PreviewLookupTimeout

// notFoundYet is what "not_found" means right after a create.
func notFoundYet() string {
	return i18n.T("the reads do not find it yet: it may still be on its way; the read of the operation says when it ends and how — do not create it again",
		"чтение его ещё не находит: он может быть ещё в пути; чтение операции скажет, когда и чем она кончится, — не создавайте его повторно")
}

// noStateRead is resource_status_note when the create's answer cannot
// name the state of resource id: the read found it with no state, or the
// cloud's answer named no resource ("").
func noStateRead(id string) string {
	if id == "" {
		return i18n.T("the cloud's answer to the create names no id of the resource, so its state is not read; the read of the operation says when it ends and what it made — do not create it again",
			"ответ облака на создание не назвал id ресурса, и его состояние не прочитано; чтение операции скажет, когда она кончится и что создала, — не создавайте его повторно")
	}
	return i18n.T("the read finds the resource with no state; the read of the operation says when it ends and how — do not create it again",
		"чтение находит ресурс без состояния; чтение операции скажет, когда и чем она кончится, — не создавайте его повторно")
}

func cloudText(m map[string]any, fields ...string) {
	var has []string
	switch u := m["_untrusted"].(type) {
	case []string:
		has = u
	case []any:
		has = stringList(u)
	}
	for _, f := range fields {
		if !slices.Contains(has, f) {
			has = append(has, f)
		}
	}
	m["_untrusted"] = has
}

// listedBy is the read of a resource with no read of its own: the item of
// list whose id is id or, without one, whose name is name; nil when none.
func listedBy(list func(context.Context) ([]map[string]any, error), id, name string) func(context.Context) (map[string]any, error) {
	return func(ctx context.Context) (map[string]any, error) {
		items, err := list(ctx)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			if id != "" && it["id"] == id || id == "" && name != "" && it["name"] == name {
				return it, nil
			}
		}
		return nil, nil
	}
}

// addResourceStatus sets resource_status of the operation m of tool: the
// status the first read of operationReads that knows the resource gives,
// "not_found" with resource_status_note when none does (a delete that is
// done, or a resource of a kind the reads do not know), and
// resource_status_error with the read's reason when a read fails otherwise.
// An operation without a resource id gets neither.
func (s *Registry) addResourceStatus(ctx context.Context, tool string, m map[string]any) {
	id, _ := m["resource_id"].(string)
	if id == "" {
		id, _ = m["resourceId"].(string)
	}
	if id == "" {
		return
	}
	for _, r := range operationReads[tool] {
		res, err := s.callTool(ctx, r.tool, map[string]any{r.arg: id})
		if err != nil {
			reason, _ := res["reason"].(string)
			if reason == clierrors.CodeNotFound {
				continue
			}
			if reason == "" {
				reason = clierrors.CodeAPIError
			}
			m["resource_status_error"] = reason
			return
		}
		for _, k := range []string{"status", "state", "phase"} {
			if v, _ := res[k].(string); v != "" {
				m["resource_status"] = v
				cloudText(m, "resource_status")
				return
			}
		}
		if r.noState != "" {
			m["resource_status_note"] = i18n.T(fmt.Sprintf("%s finds the resource; it has no state to name: %s", r.tool, r.noState),
				fmt.Sprintf("%s находит ресурс; состояния у него нет: %s", r.tool, r.noStateRU))
			return
		}
		m["resource_status_error"] = clierrors.CodeAPIError
		m["resource_status_note"] = i18n.Tf("%s finds the resource with no state: what it is now is not known; the read of the operation says when it ends and how",
			"%s находит ресурс без состояния: каков он сейчас, неизвестно; чтение операции скажет, когда и чем она кончится", r.tool)
		return
	}
	// The reads know the kind of resource the tool's create gives; a route
	// of a VPC or a backup of a cluster is not one of them.
	reads := make([]string, 0, len(operationReads[tool]))
	for _, r := range operationReads[tool] {
		reads = append(reads, r.tool)
	}
	m["resource_status"] = "not_found"
	if done, _ := m["done"].(bool); !done {
		m["resource_status_note"] = i18n.Tf("%s does not find %s yet: while the operation goes on, its resource may not be shown yet; the read of the operation says when it ends",
			"%s пока не находит %s: пока операция идёт, её ресурс может быть ещё не виден; чтение операции скажет, когда она кончится",
			strings.Join(reads, ", "), quoteCloud(id))
		return
	}
	// Done: a create's resource may still be not shown yet (312-1).
	m["resource_status_note"] = i18n.Tf("%s does not find %s: it is not shown yet, or deleted, or the operation is of a resource %s does not read (a route, a backup)",
		"%s не находит %s: ресурс ещё не виден, удалён или операция — над ресурсом, который %s не читает (маршрут, резервная копия)",
		strings.Join(reads, ", "), quoteCloud(id), strings.Join(reads, ", "))
}
