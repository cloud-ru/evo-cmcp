// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"strings"

	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
)

func notInCatalogue(what, value string, known []string, listTool string) error {
	if value == "" || len(known) == 0 {
		return nil
	}
	bare := strings.TrimPrefix(value, "v")
	for _, k := range known {
		if strings.TrimPrefix(k, "v") == bare {
			return nil
		}
	}
	return clierrors.New(clierrors.CodeNotFound,
		i18n.Tf("%s %q is not in the catalogue. Available: %s", "%s %q нет в каталоге. Есть: %s", what, value, strings.Join(known, ", ")),
		i18n.Tf("Pick one of %s.", "Выберите из %s.", listTool))
}

// listed are the non-empty string values of field in items: what a list
// names, for notInCatalogue.
func listed(items []map[string]any, field string) []string {
	var out []string
	for _, it := range items {
		if v, _ := it[field].(string); v != "" {
			out = append(out, v)
		}
	}
	return out
}
