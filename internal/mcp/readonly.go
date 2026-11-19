// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"

	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/tools"
)

func (s *Server) SetReadOnly() {
	if s.readOnly {
		return
	}
	s.readOnly = true
	s.reg.SetReadOnly()
	s.restrictKubectl(false, readOnlyWording)
	s.restrictSecrets(readOnlyWording)
	s.gated = map[string]*tools.Handler{}
	for _, t := range s.reg.Tools() {
		s.restrict("H11", t.Name, func(next tools.Handler) tools.Handler {
			inner := &next
			s.gated[t.Name] = inner
			return func(ctx context.Context, args map[string]any) (string, bool) {
				if tools.NeedsConsent(t, args) {
					return readOnlyRefusal(t.Name), true
				}
				return (*inner)(ctx, args)
			}
		})
	}
	s.relist()
}

// readOnlyRefusal is the answer of a call H11 refuses.
func readOnlyRefusal(tool string) string {
	return tools.ErrResult(clierrors.New(clierrors.CodeUsage,
		tool+" is refused: this server runs with --read-only, and the call needs the user's consent (confirmed=true), "+
			"as it changes the cloud or reveals a secret; a read-only server makes no such call, confirmed or not",
		"Calls that only read work as usual. A change needs a server without --read-only: ask its operator."))
}
