// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/httpretry"
	"github.com/cloud-ru/evo-cmcp/internal/journal"
	"github.com/cloud-ru/evo-cmcp/internal/secretsent"
)

func outer(name string, schema map[string]any, h Handler) Handler {
	return journaled(name, schema, checkedArgs(name, schema, func(ctx context.Context, args map[string]any) (string, bool) {
		ctx = secretsent.Watch(httpretry.TraceRequests(ctx))
		text, isErr := h(ctx, args)
		text = secretWriteAnswer(ctx, name, args, text, isErr)
		if isErr {
			text = withRequestID(ctx, text)
		}
		return withoutAPIStatus(text), isErr
	}))
}

type journalDepthKey struct{}

func journaled(name string, schema map[string]any, next Handler) Handler {
	props, _ := schema["properties"].(map[string]any)
	return func(ctx context.Context, args map[string]any) (string, bool) {
		depth, _ := ctx.Value(journalDepthKey{}).(int)
		start := time.Now()
		text, isErr := next(context.WithValue(ctx, journalDepthKey{}, depth+1), args)
		if !journal.Active() {
			return text, isErr
		}
		rec := journal.Record{Tool: name, Depth: depth, Error: isErr,
			MS: time.Since(start).Milliseconds(), Bytes: len(text)}
		for k := range args {
			if _, ok := props[k]; ok {
				rec.Args = append(rec.Args, k)
			} else {
				rec.UnknownArgs++
			}
		}
		sort.Strings(rec.Args)
		rec.Confirmed, _ = args["confirmed"].(bool)
		var ans struct{ Status, Reason string }
		if json.Unmarshal([]byte(text), &ans) == nil {
			rec.Preview = ans.Status == "confirmation_required"
			if isErr {
				rec.Reason = journalReason(ans.Reason)
			}
		}
		if c := liveCaller(ctx); c != nil && depth == 0 {
			rec.Confirmed = rec.Confirmed || c.consented()
			rec.Preview = rec.Preview || c.DryRun
		}
		journal.Write(rec)
		return text, isErr
	}
}

// journalReason keeps a reason that is a code of internal/errors; any
// other text is the caller's or the cloud's, and stays out.
func journalReason(r string) string {
	switch {
	case r == "":
		return ""
	case clierrors.IsCode(r), r == clierrors.ReasonInterrupted, r == clierrors.ReasonGeneral:
		return r
	}
	return "OTHER"
}
