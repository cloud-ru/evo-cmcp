// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"encoding/json"
	stderrors "errors"

	"github.com/cloud-ru/evo-cmcp/internal/i18n"
)

func JSONProblem(err error) string {
	var syntax *json.SyntaxError
	var typ *json.UnmarshalTypeError
	switch {
	case stderrors.As(err, &syntax):
		return i18n.Tf("not valid JSON (at byte %d)", "не JSON (байт %d)", syntax.Offset)
	case stderrors.As(err, &typ):
		return i18n.Tf("a value that does not fit its JSON type, e.g. a number out of range (at byte %d)",
			"значение не помещается в свой тип JSON, например число вне диапазона (байт %d)", typ.Offset)
	default:
		return i18n.T("not a JSON object", "не объект JSON")
	}
}
