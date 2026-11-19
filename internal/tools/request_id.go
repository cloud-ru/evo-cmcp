// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"strings"

	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/httpretry"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
)

func withRequestID(ctx context.Context, answer string) string {
	if !strings.Contains(answer, `"reason"`) {
		return answer
	}
	var m map[string]any
	if json.Unmarshal([]byte(answer), &m) != nil {
		return answer
	}
	status := 0
	if st, ok := m[apiStatusField].(float64); ok {
		status = int(st)
	} else if m["reason"] != clierrors.CodeAPIUnreachable {
		return answer
	}
	msg, _ := m["error"].(string)
	f, ok := httpretry.FailedRequest(ctx, status)
	if !ok || f.Sent == "" || strings.Contains(msg, f.Sent) {
		return answer
	}
	named := i18n.Tf(" (X-Request-Id %s)", " (X-Request-Id %s)", f.Sent)
	if f.Answered != "" && f.Answered != f.Sent {
		named = i18n.Tf(" (X-Request-Id %s, the answer's %s)", " (X-Request-Id %s, в ответе %s)", f.Sent, f.Answered)
	}
	m["error"] = msg + named
	if c := callerOf(ctx); c != nil && c.Err != nil {
		c.Err = clierrors.Suffix(c.Err, named)
	}
	return toJSON(m)
}
