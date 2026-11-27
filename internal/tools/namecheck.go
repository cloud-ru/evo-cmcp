// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	stderrors "errors"
	"fmt"
	"strings"

	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
	"github.com/cloud-ru/evo-cmcp/internal/pager"
	"github.com/cloud-ru/evo-cmcp/internal/provision"
	"github.com/cloud-ru/evo-cmcp/internal/receipt"
)

// namedResources requires a name on every row before deciding that a name
// is available. A display list may otherwise include an unnamed resource.
func namedResources(items []map[string]any, err error) ([]map[string]any, error) {
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if strings.TrimSpace(stringArg(item, "name")) == "" {
			return nil, pager.Incomplete(i18n.T("a resource in the list has no name to check", "у ресурса в списке нет имени для проверки"))
		}
	}
	return items, nil
}

func takenName(items []map[string]any, name string) (id, status string, ok bool) {
	for _, m := range items {
		if n, _ := m["name"].(string); n != name {
			continue
		}
		st, _ := m["status"].(string)
		if st == "" {
			st, _ = m["state"].(string)
		}
		if statusGone(st) {
			continue // gone: the name is free again
		}
		id, _ = m["id"].(string)
		return id, st, true
	}
	return "", "", false
}

func statusGone(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "DELETED", "ERASED":
		return true
	}
	return false
}

func nameFree(ctx context.Context, list func(context.Context) ([]map[string]any, error), kind, name, getHint string) (string, bool) {
	items, err := list(ctx)
	if err != nil {
		return errResult(fmt.Errorf(i18n.T("check the name is free: %w", "проверка, что имя свободно: %w"), err)), true
	}
	if id, st, ok := takenName(items, name); ok {
		return nameTakenResult(kind, name, id, st, getHint)
	}
	return "", false
}

func nameTakenResult(kind, name, id, status, getHint string) (string, bool) {
	if status == "" {
		status = "unknown"
	}
	return errAnswer(clierrors.New(clierrors.CodeUsage,
		i18n.Tf("a %s named %s already exists (id %s, status %s); a second one would be billed separately", "%s с именем %s уже есть (id %s, статус %s); второй оплачивался бы отдельно", kind, quoteCloud(name), quoteCloud(id), quoteCloud(status)),
		i18n.Tf("Use the existing one (%s), or pick another name.", "Используйте существующий (%s) или выберите другое имя.", getHint)),
		map[string]any{"existing_id": id}), true
}

// noID is the error of a create the API answered with success but no id
// of what it made (operation or resource): the API's fault, API_ERROR, not
// the caller's.
func noID(what string) error {
	return clierrors.New(clierrors.CodeAPIError,
		i18n.Tf("the API answered no %s id", "в ответе API нет идентификатора (тип «%s»)", what), "")
}

func noReceipt(err error, read string) error {
	var ce *clierrors.CLIError
	if !stderrors.Is(err, receipt.ErrUnusable) || !stderrors.As(err, &ce) {
		return err
	}
	return clierrors.Wrap(ce.Code, ce.Message, read, err)
}

func lostCreate(ctx context.Context, tool string, list func(context.Context) ([]map[string]any, error), kind, name string, created map[string]any, getHint string, createErr error) (string, bool) {
	c := decls[tool].Created
	idField, listTool := c.lostField(), c.list
	if idField == "" || listTool == "" {
		panic("tools: lostCreate for " + tool + ", whose Decl.Created declare no id field or list")
	}
	if st := apiStatus(createErr); st >= 400 && st < 500 {
		return errResult(createErr), true
	}
	items, err := list(ctx)
	var id, status string
	var ok bool
	if err == nil {
		id, status, ok = takenName(items, name)
	}
	// A server-assigned identity must actually be in the recovered row.
	// A resource keyed by its input (such as a secret path) is identified by that match.
	if err != nil || (ok && c.arg == "" && strings.TrimSpace(id) == "") {
		return errAnswer(createErr, map[string]any{"warning": i18n.Tf(
			"the %s %q may have been made and be billed: check %s by its name before any repeat — a repeat while it is not listed there may make a second",
			"%s %q мог быть создан и оплачиваться: до любого повтора проверьте %s по имени — повтор, пока его там нет, может создать второй",
			kind, name, listTool)}), true
	}
	if !ok {
		// The request went out and the list does not show what it may
		// have made: the answer sends to the read, not to a repeat.
		return errAnswer(createErr, map[string]any{"warning": i18n.Tf(
			"the create request was sent and got no answer: the %s %q may have been made though %s does not show it now — check %s again before any repeat",
			"запрос на создание отправлен, ответ не получен: %s %q мог быть создан, хотя %s сейчас его не показывает — до любого повтора проверьте %s ещё раз",
			kind, name, listTool, listTool)}), true
	}
	answer := "creating"
	if resourceFailed(status) {
		// A script that reads status alone must not wait for it to come up.
		answer = "failed"
	}
	out := map[string]any{"status": answer, idField: id, "resource_status": status, "created": created,
		"warning": i18n.Tf("the create answer did not come back, but the %s %q exists (id %s, status %s) and is billed", "ответ на создание не пришёл, но %s %q есть (id %s, статус %s) и оплачивается", kind, name, quoteCloud(id), quoteCloud(status)),
		"note":    i18n.Tf("Poll %s for its status.", "Статус — %s.", getHint)}

	cloudText(out, "resource_status")
	return toJSON(out), false
}

func needSpecResult(specsTool, specID string, cpu, ram int, fields map[string]any) (string, bool) {
	msg := i18n.T("a specification is needed: pass specification_id, or cpu+ram matching one of available_specifications",
		"нужна конфигурация: укажите specification_id или cpu+ram одной из available_specifications")
	switch {
	case specID != "":
		msg = i18n.Tf("specification_id %q is not one of available_specifications",
			"specification_id %q нет среди available_specifications", specID)
	case cpu > 0 && ram > 0:
		msg = i18n.Tf("no specification with cpu=%d and ram=%d; pick one of available_specifications",
			"нет конфигурации с cpu=%d и ram=%d; выберите одну из available_specifications", cpu, ram)
	}
	fields["status"] = "need_specification"
	return errAnswer(clierrors.New(clierrors.CodeUsage, msg,
		i18n.Tf("Pass specification_id (or cpu+ram) of one of available_specifications; %s lists them all.",
			"Укажите specification_id (или cpu+ram) одной из available_specifications; полный список — %s.", specsTool)), fields), true
}

// resourceFailed reports a status of a resource that will not come up:
// FAILED of the managed services, ERROR or error_<step> of compute
// (provision.VMFailed).
func resourceFailed(status string) bool {
	return strings.Contains(strings.ToLower(status), "fail") || provision.VMFailed(status)
}
