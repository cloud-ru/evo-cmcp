// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"strings"

	"github.com/cloud-ru/evo-cmcp/internal/i18n"
)

func DeleteWayOut(status, id string) (warning, hint string) {
	switch up := strings.ToUpper(status); {
	case PassingDeleteState(status):
		return i18n.Tf("it is still being created (%s): Cloud.ru refuses the delete until it is ready",
				"он ещё создаётся (%s): Cloud.ru не удалит его, пока он не будет готов", status),
			i18n.Tf("Cloud.ru does not delete a resource while it is being created (%s): wait until it is ready, then delete it again.",
				"Cloud.ru не удаляет ресурс, пока он создаётся (%s): дождитесь, пока он будет готов, и повторите удаление.", status)
	case up == "DELETING":
		return i18n.Tf("Cloud.ru is already deleting it (%s); ", "Cloud.ru уже удаляет его (%s); ", status) + FailedDeleteNote(id),
			i18n.Tf("Cloud.ru is already deleting it (%s): wait and check that it is gone; if it stays %s, delete it in the Cloud.ru console, or contact Cloud.ru support with its id %q.",
				"Cloud.ru уже удаляет его (%s): подождите и проверьте, что его нет; если он остаётся в %s, удалите его в консоли Cloud.ru или обратитесь в поддержку Cloud.ru с его id %q.", status, status, id)
	case finalDeleteStates[up]:
		return i18n.Tf("it is in %s: ", "он в состоянии %s: ", status) + FailedDeleteNote(id),
			i18n.Tf("Cloud.ru does not delete a resource in %s through its API: delete it in the Cloud.ru console, or contact Cloud.ru support with its id %q.",
				"Cloud.ru не удаляет ресурс в состоянии %s через API: удалите его в консоли Cloud.ru или обратитесь в поддержку Cloud.ru с его id %q.", status, id)
	}
	return "", ""
}

func FailedDeleteNote(id string) string {
	return i18n.Tf("Cloud.ru may refuse to delete it in this state; if it does, contact Cloud.ru support with its id %q.",
		"Cloud.ru может отказать в удалении в этом состоянии; при отказе обратитесь в поддержку Cloud.ru с его id %q.", id)
}

// finalDeleteStates are the states a resource does not leave by itself and
// Cloud.ru does not delete it in: the way out is the console or support,
// not a wait (DeleteWayOut).
var finalDeleteStates = map[string]bool{"FAILED": true, "ERROR": true, "ERROR_DELETING": true}

// FinalDeleteState reports whether status is one of finalDeleteStates.
func FinalDeleteState(status string) bool { return finalDeleteStates[strings.ToUpper(status)] }

func PassingDeleteState(status string) bool {
	switch strings.ToUpper(status) {
	case "DEPLOYING", "CREATING", "PROVISIONING":
		return true
	}
	return false
}

func DeletingState(status string) bool {
	switch strings.ToUpper(status) {
	case "DELETING", "ERASING":
		return true
	}
	return false
}
