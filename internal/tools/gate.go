// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"maps"

	"github.com/cloud-ru/evo-cmcp/internal/confirm"
	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/httpretry"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
)

// Plan is what a call of a gated tool will do, found out without a write.
type Plan struct {
	// Preview is the answer the consent is asked on, without its status:
	// the gate answers it as confirmation_required.
	Preview map[string]any

	PreviewOf func(ctx context.Context) (map[string]any, error)
	// Execute makes the call; the gate runs it only with consent.
	Execute func(ctx context.Context) (string, bool)
	// answer is the whole answer of a call with nothing to consent to.
	answer    string
	answerErr bool
}

// answered is the plan of a call with nothing to consent to: its answer,
// given with no question whatever confirmed says.
func answered(text string, isErr bool) *Plan {
	return &Plan{answer: text, answerErr: isErr}
}

// Planner plans a call of a gated tool. An error refuses the call before
// the question.
type Planner func(ctx context.Context, args map[string]any) (*Plan, error)

// gated is the handler of tool name on its planner: the plan first, then
// the question when this call needs consent (NeedsConsent), then the
// execution.
func (r *Registry) gated(name string, plan Planner) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		p, err := plan(ctx, args)
		if err != nil {
			return errResult(err), true
		}
		if p.Execute == nil {
			return p.answer, p.answerErr
		}
		t, _ := r.Lookup(name)
		if confirmed, _ := args["confirmed"].(bool); !confirmed && NeedsConsent(t, args) {
			preview := p.Preview
			if p.PreviewOf != nil {
				if preview, err = p.PreviewOf(ctx); err != nil {
					return errResult(err), true
				}
			}
			out := map[string]any{"status": "confirmation_required"}
			for k, v := range preview {
				out[k] = v
			}
			return toJSON(out), true
		}
		return p.Execute(ctx)
	}
}

// engineGated is the gate of a tool the engine drives itself (deploy, the
// destroy of the deployment): the Caller of a CLI command asks for consent
// where the engine reaches its gate (Caller.Gate: live), after the checks
// the engine makes first; any other call gates here on confirmed —
// without it, preview answers; with it, execute runs.
func (r *Registry) engineGated(ctx context.Context, name string, args map[string]any,
	live func(*Caller) (string, bool), preview, execute func() (string, bool)) (string, bool) {
	if c := liveCaller(ctx); c != nil {
		// The engine asks at its own gate: the intent is written on the
		// yes, before the engine writes anything (Caller.ask); a call with
		// confirmed has it from intended already.
		var end func(string)
		prev := c.consent
		c.consent = func(p *confirm.Preview) error {
			if confirmed, _ := args["confirmed"].(bool); end != nil || confirmed {
				return nil
			}
			// Record the target the engine actually showed under its lock.
			// Use the same identity and settlement rules as an addressed call.
			journalArgs := args
			if p != nil && len(p.TargetArgs) > 0 {
				journalArgs = make(map[string]any, len(args)+len(p.TargetArgs))
				maps.Copy(journalArgs, args)
				for key, value := range p.TargetArgs {
					journalArgs[key] = value
				}
			}
			e, err := r.beginIntent(ctx, name, journalArgs)
			end = e
			return err
		}
		defer func() { c.consent = prev }()
		text, isErr := live(c)
		if end != nil {
			end(text)
		}
		return text, isErr
	}
	t, _ := r.Lookup(name)
	if confirmed, _ := args["confirmed"].(bool); !confirmed && NeedsConsent(t, args) {
		return preview()
	}
	return execute()
}

func (r *Registry) intended(t Tool, next Handler) Handler {
	if !takesConsent(t) {
		return next
	}
	return func(ctx context.Context, args map[string]any) (string, bool) {
		// A step of a stack is a call one level deeper (journaled): the
		// intent of the call the person consented to covers it, and the
		// job of the stack records its steps.
		depth, _ := ctx.Value(journalDepthKey{}).(int)
		if depth > 1 {
			return next(ctx, args)
		}
		// The holder of what the call reads and says of its records: the
		// call of a tool has it (budgeted); a layer called on its own gets
		// its own, so the mark of its first write is heard (markSent).
		if _, ok := ctx.Value(callRecordsKey{}).(*callRecords); !ok {
			ctx = context.WithValue(ctx, callRecordsKey{}, &callRecords{})
		}
		// No directory for the record of the call: no question is asked
		// about a call that would be refused, and none is sent.
		if r.stateDirUndeclared() {
			return errResult(errNoStateDir()), true
		}
		// The same create before was taken and goes on: its progress is
		// read, with or without consent, and no second create is sent.
		if text, isErr, ok := r.repeatOfMade(ctx, t.Name, args); ok {
			return text, isErr
		}
		if confirmed, _ := args["confirmed"].(bool); !confirmed || !NeedsConsent(t, args) {
			text, isErr := next(ctx, args)
			r.settleHeld(ctx, t.Name, args, text)
			text = r.questionNamesTheCut(ctx, t.Name, args, text)
			return r.namedKept(ctx, t.Name, args, text), isErr
		}

		holder := ctx
		ctx = httpretry.CountWrites(ctx, func() (bool, error) {
			return markSent(holder, func() []string { return r.partiesOf(holder, t.Name, args) })
		})

		if _, _, err := r.resourceOf(ctx, t.Name, args); err != nil {
			return errResult(err), true
		}
		if text, asked := r.askAgainAfterACut(ctx, t.Name, args, func(ctx context.Context) (string, bool) {
			return next(ctx, withoutConsent(args))
		}); asked {
			return text, true
		}

		var end func(string)
		if held, goesOn := r.goesOn(ctx, t.Name, args); goesOn {
			end = func(answer string) { cutSendOf(ctx, held, answer) }
		} else {
			var err error
			if end, err = r.beginIntent(ctx, t.Name, args); err != nil {
				return errResult(err), true
			}
		}

		returned := false
		defer func() {
			if !returned {
				end(toJSON(map[string]any{"status": "error", "reason": clierrors.ReasonInterrupted,
					"error": i18n.T("the call stopped before it returned", "вызов остановился, не вернувшись")}))
			}
		}()
		text, isErr := next(ctx, args)
		returned = true
		// The mark of the first write could not be written: nothing went
		// out, and the call says that, not the handler's error of a request
		// that never left.
		if err := httpretry.WriteRefused(ctx); err != nil {
			text, isErr = errResult(err), true
		}
		end(text)
		// A held record the call names by another argument than its step
		// (a name, where the record has the id).
		r.settleHeld(ctx, t.Name, args, text)
		return r.namedKept(ctx, t.Name, args, text), isErr
	}
}

// withoutConsent is the same call without the consent it was given: the
// question it answers is the preview of what the call would do.
func withoutConsent(args map[string]any) map[string]any {
	out := maps.Clone(args)
	delete(out, "confirmed")
	return out
}

func nextSameCall(answer map[string]any, tool string, args map[string]any) {
	next := maps.Clone(args)
	delete(next, "confirmed")
	nextStep(answer, tool, next)
}
