// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/cloud-ru/evo-cmcp/internal/engine"
	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
	"github.com/cloud-ru/evo-cmcp/internal/intent"
)

func (s *Registry) deletePlan(tool, en, ru, id string, get func() (map[string]any, error), execute func(context.Context) (string, bool)) *Plan {
	m, err := get()
	if err != nil {
		return answered(s.deleteFailed(tool, id, err))
	}
	name, _ := m["name"].(string)
	status, _ := m["status"].(string)
	if status == "" {
		status, _ = m["state"].(string)
	}
	label := id
	if name != "" {
		label = fmt.Sprintf("%s (%s)", quoteCloud(name), id)
	}
	decl := decls[tool].Delete
	state := strings.TrimPrefix(status, decl.statePrefix)
	unseen := decl.creatingUnseen != "" && engine.PassingDeleteState(state) ||
		decl.failedUnseen != "" && inError(state)
	if unseen {
		state = ""
	}
	if statusGone(state) {
		return answered(s.deleteFailed(tool, id, clierrors.New(clierrors.CodeNotFound,
			i18n.Tf("%s is %s", "%s в состоянии %s", label, status), "")))
	}
	target := map[string]any{"id": id, "name": name, "status": status}
	if refused := passingRefusal(label, id, state, target); refused != nil {
		return refused
	}
	if deleting := alreadyDeleting(label, state, target); deleting != nil {
		return deleting
	}
	out := map[string]any{
		"message": i18n.Tf(en, ru, label),
		"target":  target,
		// A kind the folder's state does not record: unknown, said by the
		// one rule of every delete preview (P3).
		"provenance": engine.ProvenanceOf(decls[tool].Delete.what, id, time.Time{}, nil, nil, nil),
	}
	if warning, _ := engine.DeleteWayOut(state, id); warning != "" {
		out["warning"] = warning
	} else if unseen {
		out["warning"] = i18n.Tf("it is %s: whether Cloud.ru deletes a %s in this state no run has shown — the outcome of the delete is unknown; a refusal names the platform's answer",
			"он в состоянии %s: удаляет ли Cloud.ru ресурс (%s) в этом состоянии, ни один прогон не показал — исход удаления неизвестен; отказ назовёт ответ платформы",
			quoteCloud(status), i18n.T(decl.what, decl.whRU))
	}
	if unseen {
		return &Plan{Preview: out, Execute: withStatus(execute, tool, status, id)}
	}
	return &Plan{Preview: out, Execute: withWayOut(execute, state, status, id)}
}

// inError reports whether state is an error of the resource: one Cloud.ru
// does not delete in (engine.FinalDeleteState), or another error state of
// the service, such as error_creating of a backup.
func inError(state string) bool {
	return engine.FinalDeleteState(state) || strings.HasPrefix(strings.ToUpper(state), "ERROR")
}

func withStatus(execute func(context.Context) (string, bool), tool, status, id string) func(context.Context) (string, bool) {
	decl := decls[tool].Delete
	read := strings.TrimSuffix(tool, "_delete") + "_get"
	readEN, readRU := read, read
	if _, ok := commandOf(read); ok {
		readRU = cliCommand(read)
	} else {
		readEN, readRU = "a read of it", "чтение"
	}
	return func(ctx context.Context) (string, bool) {
		text, isErr := execute(ctx)
		if !isErr {
			return text, isErr
		}
		var m map[string]any
		if json.Unmarshal([]byte(text), &m) != nil {
			return text, isErr
		}
		m["resource_status"] = status
		reason, _ := m["reason"].(string)
		switch code := answerHTTPStatus(text); {
		case (code == http.StatusUnauthorized || code == http.StatusForbidden) && !notTheState[reason]:
			m["hint"] = i18n.T(
				fmt.Sprintf("Cloud.ru denied the delete to this key (HTTP %d): the key was not accepted, or it or the role of its service account has no right to delete the %s; it stays, and a billable one is still billed. Try the delete with a key that has that right.", code, decl.what),
				fmt.Sprintf("Cloud.ru отказал этому ключу в удалении (HTTP %d): ключ не принят либо у него или у роли его сервисного аккаунта нет права удалять ресурс (%s); ресурс остаётся, платный — тарифицируется. Попробуйте удалить ключом с этим правом.", code, decl.whRU))
		case intent.DefiniteRefusal(code) && !notTheState[reason]:
			m["hint"] = i18n.T(
				fmt.Sprintf("Cloud.ru refused the delete of the %s in %s: it stays, and a billable one is still billed; the same call gets the same refusal while its state holds. What to do with it is the person's decision; the way is Cloud.ru support, with its id %s.", decl.what, quoteCloud(status), quoteCloud(id)),
				fmt.Sprintf("Cloud.ru отклонил удаление: ресурс (%s) в состоянии %s остаётся, платный ресурс тарифицируется; тот же вызов получит тот же отказ, пока состояние не сменится. Что с ним делать, решает человек; путь — поддержка Cloud.ru с id ресурса %s.", decl.whRU, quoteCloud(status), quoteCloud(id)))
		case !notTheState[reason] || reason == clierrors.CodeAPIUnreachable:
			m["hint"] = i18n.T(
				fmt.Sprintf("The outcome of the delete is unknown: the request may have been taken. %s %s shows whether the %s is gone.", readEN, id, decl.what),
				fmt.Sprintf("Исход удаления неизвестен: запрос мог быть принят. Удалён ли ресурс (%s), покажет %s %s.", decl.whRU, readRU, id))
		}
		return toJSON(m), true
	}
}

func passingRefusal(label, id, status string, target map[string]any) *Plan {
	if !engine.PassingDeleteState(status) {
		return nil
	}
	_, hint := engine.DeleteWayOut(status, id)
	return answered(errAnswer(clierrors.New(clierrors.CodeUsage,
		i18n.Tf("%s is still being created (%s): Cloud.ru does not delete it until it is ready; nothing was done",
			"%s ещё создаётся (%s): Cloud.ru не удалит его, пока он не будет готов; ничего не сделано", label, status), hint),
		map[string]any{"resource_status": target["status"], "target": target}), true)
}

func alreadyDeleting(label, status string, target map[string]any) *Plan {
	if !engine.DeletingState(status) {
		return nil
	}
	return answered(toJSON(map[string]any{
		"status":           "deleting",
		"already_deleting": true,
		"resource_status":  target["status"],
		"target":           target,
		"message": i18n.Tf("%s is already being deleted by Cloud.ru (%s); nothing was sent — check later that it is gone",
			"%s уже удаляется Cloud.ru (%s); ничего не отправлено — позже проверьте, что его нет", label, status),
	}), false)
}

var notTheState = map[string]bool{
	clierrors.CodeAuthRequired:   true,
	clierrors.CodeAuthExpired:    true,
	clierrors.CodeAuthFailed:     true,
	clierrors.CodeAPIRateLimited: true,
	clierrors.CodeAPIUnreachable: true,
}

func withWayOut(execute func(context.Context) (string, bool), state, status, id string) func(context.Context) (string, bool) {
	_, hint := engine.DeleteWayOut(state, id)
	if hint == "" {
		return execute
	}
	return func(ctx context.Context) (string, bool) {
		text, isErr := execute(ctx)
		if !isErr {
			return text, isErr
		}
		var m map[string]any
		if json.Unmarshal([]byte(text), &m) != nil {
			return text, isErr
		}
		m["resource_status"] = status
		if reason, _ := m["reason"].(string); !notTheState[reason] {
			m["hint"] = hint
		}
		return toJSON(m), true
	}
}
