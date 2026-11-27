// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
)

// deleteTarget is what a delete tool deletes: the argument naming it, the
// argument naming the resource it lives in, and its kind for the answer.
type deleteTarget struct {
	arg        string // names the resource
	parent     string // names the resource it lives in; "" for none
	what, whRU string

	statePrefix string

	creatingUnseen string
	// failedUnseen is why a resource in a failed state gets no warning of
	// a refusal and no way out of one (engine.DeleteWayOut): no run has
	// shown the service refuse that delete. "" is the rule of every
	// delete.
	failedUnseen string
}

const unseenBeforeProbe = "no run has shown the refusal: after two runs of probe Г-12 (release plan 1.9)"

// isGone reports whether err says the resource is not there: its reason,
// as the error answer gives it, is NOT_FOUND or VM_NOT_FOUND.
func isGone(err error) bool {
	switch reason, _, _ := errReason(err, nil); reason {
	case clierrors.CodeNotFound, clierrors.CodeVMNotFound:
		return true
	}
	return false
}

// alreadyDeleted is the answer of a delete tool to a call whose resource ref
// the cloud does not have (err says so, isGone); key names ref in the
// answer ("" for the tool's target argument). ok is false for any other
// error, which stays the tool's error. A parent that is not there is not
// asked here: the handler answers it as the error it is. Bound to its
// projects, the registry answers NOT_FOUND instead (isErr true).
func (s *Registry) alreadyDeleted(tool, key, ref string, err error) (out string, isErr, ok bool) {
	if err == nil || !isGone(err) {
		return "", false, false
	}
	t, ok := deleteOf(tool)
	if !ok {
		panic("tools: alreadyDeleted for " + tool + ", which Decl.Delete does not declare")
	}
	if key == "" {
		key = t.arg
	}
	what := i18n.T(t.what, t.whRU)
	if s.projectBound {
		return errResult(boundDeleteNotFound(tool, ref)), true, true
	}
	return toJSON(map[string]any{
		"status":  "already_deleted",
		"existed": false,
		key:       ref,
		"note": i18n.Tf("no %s %s: it was deleted earlier, or the %s is wrong — check it in the list before relying on it",
			"%s %s отсутствует: удаление уже выполнено раньше или %s указан неверно — проверьте по списку, прежде чем на это полагаться",
			what, ref, key),
	}), false, true
}

func boundDeleteNotFound(tool, ref string) error {
	t, _ := deleteOf(tool)
	return notInProject(&ownerKind{what: i18n.T(t.what, t.whRU),
		listTool: i18n.T("the list of the project's resources", "списка ресурсов проекта")}, ref)
}

// deleteFailed answers a delete tool's call that failed on its resource:
// already_deleted when the cloud does not have it, else the error.
func (s *Registry) deleteFailed(tool, ref string, err error) (string, bool) {
	return s.deleteFailedAs(tool, "", ref, err)
}

// deleteFailedAs is deleteFailed for a resource named by another argument
// than the tool's target one (key).
func (s *Registry) deleteFailedAs(tool, key, ref string, err error) (string, bool) {
	if out, isErr, ok := s.alreadyDeleted(tool, key, ref, err); ok {
		return out, isErr
	}
	return errResult(err), true
}
