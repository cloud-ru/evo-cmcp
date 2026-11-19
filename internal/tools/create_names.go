// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"

	"github.com/cloud-ru/evo-cmcp/internal/i18n"
)

// createName is what names the resource a create makes.
type createName struct {
	args []string // the arguments that name the new resource
	// rule is the service's own rule for the name, which the handler
	// checks before the cloud (stricter than plainName); "" for
	// plainName, which the registry checks before the handler.
	rule       string
	what, whRU string
}

func checkedCreateName(name string, next Handler) Handler {
	c, ok := createNameOf(name)
	if !ok || c.rule != "" {
		return next
	}
	return func(ctx context.Context, args map[string]any) (string, bool) {
		for _, a := range c.args {
			if v, ok := args[a].(string); ok && v != "" {
				if err := plainName(i18n.T(c.what, c.whRU), a, v); err != nil {
					return errResult(err), true
				}
			}
		}
		return next(ctx, args)
	}
}
