// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	stderrors "errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/cloud-ru/evo-cmcp/internal/engine"
	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
)

const madeLimit = 2 * time.Minute

// made is a resource a create asked for that the platform makes after it
// answers: the answer carries it on its way (a subnet creating, a SNAT
// gateway CREATING).
type made struct {
	// tool is the create; list the list tool whose «ids» filter reads the
	// resource (the next step).
	tool, list string
	// field is the resource's key in the answer, idField the key of its
	// id.
	field, idField string
	// what names the resource at the start of a sentence.
	what, whatRU string
	// read lists the resources the list tool shows.
	read                             func(ctx context.Context) ([]map[string]any, error)
	note, noteRU, pending, pendingRU string
	unknown, unknownRU               string
}

func madeAnswer(ctx context.Context, eng *engine.Engine, res map[string]any, m made) (string, bool) {
	res["status"] = "created"
	item, _ := res[m.field].(map[string]any)
	id, _ := item["id"].(string)
	if madeKind(madeState(item)) == stateMade {
		madeNote(res, m, true)
		return toJSON(res), false
	}
	listed := false
	last, _, _, reads, err := waitState(ctx, eng, madeLimit,
		func(ctx context.Context) (string, bool, error) {
			items, err := m.read(ctx)
			if err != nil {
				// A read that failed is no state: the wait reads again.
				return "", false, nil
			}
			listed = false
			for _, it := range items {
				if iid, _ := it["id"].(string); iid == id {
					item, listed = it, true
					return madeState(it), true, nil
				}
			}
			return "", false, nil
		},
		func(st string) string {
			return i18n.Tf("%s %s: %s, waiting until it is made…", "%s %s: %s, жду завершения создания…",
				i18n.T(m.what, m.whatRU), quoteCloud(id), quoteCloud(st))
		},
		func(st string) (bool, error) { return madeKind(st) != stateMaking, nil })
	word := "unknown"
	if listed {
		word = quoteCloud(last)
	} else {
		last = "unknown"
	}
	res[m.idField] = id
	kind := madeKind(last)
	madeNote(res, m, false)
	switch {
	case err == nil && listed && kind == stateMade:
		res[m.field] = mergedItem(res[m.field], item)
		madeNote(res, m, true)
		return toJSON(res), false
	case err == nil && listed && kind == stateFailed:
		delete(res, "note")
		res["status"] = "failed"
		res[m.field] = mergedItem(res[m.field], item)
		return errAnswer(clierrors.New(clierrors.CodeAPIError,
			i18n.Tf("%s %s: Cloud.ru did not finish making it, state %s", "%s %s: Cloud.ru не завершил создание, состояние — %s", i18n.T(m.what, m.whatRU), quoteCloud(id), quoteCloud(last)),
			madeShowsText(m, id)+" "+i18n.Tf("Whether to delete it (%s) — ask the person.",
				"Удалять ли (%s) — спросите человека.", decls[m.tool].Created.delete)), res), true
	}
	res["status"] = "creating"
	if !listed || kind == stateOther {
		res["status"] = "waiting"
	}
	reads.put(res)
	res[m.field+"_state"] = last
	if listed {
		cloudText(res, m.field+"_state")
		res[m.field] = mergedItem(res[m.field], item)
	} else {
		res[m.field] = mergedItem(res[m.field], nil)
	}
	if (!listed || kind == stateOther) && m.unknown != "" {
		res["note"] = i18n.T(m.unknown, m.unknownRU)
	}
	res["next"] = i18n.Tf("%s %s is %s: the platform has not finished making it.",
		"%s %s — %s: платформа ещё не завершила создание.", i18n.T(m.what, m.whatRU), quoteCloud(id), word)
	switch {
	case !listed:
		res["next"] = i18n.Tf("%s %s is not in the list evo read last: evo does not know its state.",
			"%s %s нет в списке, который evo прочитал последним: evo не знает его состояния.", i18n.T(m.what, m.whatRU), quoteCloud(id))
	case kind == stateOther:
		// A state of no list: evo does not tell whether it is made.
		res["next"] = i18n.Tf("%s %s is %s: evo does not know this state as made or being made.",
			"%s %s — %s: evo не знает, сделан ли ресурс в этом состоянии.", i18n.T(m.what, m.whatRU), quoteCloud(id), word)
	}
	shows, command := madeShows(m, id)
	res["next"] = res["next"].(string) + " " + shows
	if command {
		nextStep(res, m.list, map[string]any{"ids": []any{id}})
	}
	if stderrors.Is(err, context.Canceled) {
		return errAnswer(fmt.Errorf("%s: %w", i18n.Tf("%s %s: the wait was cancelled, the platform goes on making it",
			"%s %s: ожидание прервано, платформа продолжает создание", i18n.T(m.what, m.whatRU), quoteCloud(id)), err), res), true
	}
	return toJSON(res), false
}

// madeNote sets the note of m in res: of a resource made when done, else
// the one that does not say it is created.
func madeNote(res map[string]any, m made, done bool) {
	switch {
	case done && m.note != "":
		res["note"] = i18n.T(m.note, m.noteRU)
	case m.pending != "":
		res["note"] = i18n.T(m.pending, m.pendingRU)
	}
}

// The kinds of the state of a resource a create makes (madeKind).
const (
	stateMade = iota
	stateMaking
	stateFailed
	stateOther
)

var (
	madeStates   = []string{"created", "updating"}
	makingStates = []string{"creating"}
)

// madeKind is the kind of the state st, whatever its case.
func madeKind(st string) int {
	l := strings.ToLower(st)
	switch {
	case slices.Contains(madeStates, l):
		return stateMade
	case slices.Contains(makingStates, l):
		return stateMaking
	case strings.HasPrefix(l, "error"):
		return stateFailed
	}
	return stateOther
}

// madeState is the state of a resource as its item names it: state, else
// status (a list item may carry both, a create's answer one).
func madeState(item map[string]any) string {
	if st, _ := item["state"].(string); st != "" {
		return st
	}
	st, _ := item["status"].(string)
	return st
}

func mergedItem(was any, read map[string]any) map[string]any {
	out := map[string]any{}
	if m, ok := was.(map[string]any); ok {
		maps.Copy(out, m)
	}
	delete(out, "state")
	delete(out, "status")
	maps.Copy(out, read)
	return out
}

func madeShows(m made, id string) (string, bool) {
	if isUUID(id) {
		return i18n.Tf("%s ids=[%s] shows its state.", "Состояние показывает %s ids=[%s].", m.list, id), true
	}
	return i18n.Tf("%s shows its state; the id %s is not of the form of an id, so no command is named with it.",
		"Состояние показывает %s; id %s не той формы, поэтому команда с ним не названа.", m.list, quoteCloud(id)), false
}

// madeShowsText is the text of madeShows alone: a refusal names no next
// step.
func madeShowsText(m made, id string) string {
	s, _ := madeShows(m, id)
	return s
}
