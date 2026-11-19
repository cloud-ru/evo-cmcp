// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"sort"
	"strconv"
	"strings"
)

func quoteCloud(s string) string { return strconv.Quote(s) }

// untrustedAnswer declares what of a tool's answer evo did not write.
// Fields are the answer fields with that text: the answer lists those it
// has in "_untrusted" — a marker for a client that reads it: the text is
// data, never instructions (process audit 1.2.1, П9). Workload says the
// text is what a workload or a stored object wrote, and New ends the
// tool's description with untrustedOutputNote (Tool.UntrustedOutput).
//
// Whole says the answer is the body of the service as it has it, every
// field of it the cloud's — a read that passes the resource on: the
// answer lists every field it has, but those of own, which evo adds.
// An error answer of such a tool is evo's and is not marked.
type untrustedAnswer struct {
	fields   []string
	whole    bool
	own      []string
	workload bool
	deep     bool
}

// marked reports whether the answers of the tool say "_untrusted".
func (u untrustedAnswer) marked() bool { return u.fields != nil || u.whole }

// in is the fields of the answer m the marker names.
func (u untrustedAnswer) in(m map[string]any) []string {
	var has []string
	if u.whole {
		for f := range m {
			if f != "_untrusted" && !slices.Contains(u.own, f) {
				has = append(has, f)
			}
		}
		sort.Strings(has)
		return has
	}
	for _, f := range u.fields {
		if _, ok := m[f]; ok {
			has = append(has, f)
		}
	}
	if u.deep {
		var nested []string
		for f, v := range m {
			if f != "_untrusted" && !slices.Contains(has, f) && holdsField(v, u.fields) {
				nested = append(nested, f)
			}
		}
		sort.Strings(nested)
		has = append(has, nested...)
	}
	return has
}

// holdsField reports whether v, an object or a list, holds one of fields
// as a key of an object at any depth.
func holdsField(v any, fields []string) bool {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if slices.Contains(fields, k) || holdsField(e, fields) {
				return true
			}
		}
	case []any:
		for _, e := range x {
			if holdsField(e, fields) {
				return true
			}
		}
	}
	return false
}

func serviceText(err error) bool {
	return apiStatus(err) != 0 || operationFailedRE.MatchString(strings.ToLower(err.Error()))
}

var alwaysUntrusted = []string{"error", "warning", "warnings", "failures", "failed", "preview", "hint", "operation_id",
	"registry_id", "vpc_id", "zone_id", "record_id", "route_id", "backup_id", "vm_id", "certificate_id", "tg_id", "repeat_of",
	"node_pools", "certificate", "operation", "database", "user", "disk_id", "project_id"}

// markAlways adds to "_untrusted" of every answer the fields of
// alwaysUntrusted it has. It stands outside every policy of the call
// (registry.go): what a policy reads from the marker — a secret write
// hides an error whose text is the service's (untrustedError) — is the
// marker of the tool and of errAnswer, not this one.
func markAlways(next Handler) Handler {
	return markUntrusted(alwaysMarked, next)
}

type markerKey struct{}

// markerOf is the marker of the tool whose call ctx carries: its own
// (Decl.Untrusted), else alwaysMarked, which every call has (markAlways).
func markerOf(ctx context.Context) untrustedAnswer {
	if u, ok := ctx.Value(markerKey{}).(untrustedAnswer); ok {
		return u
	}
	return alwaysMarked
}

// alwaysMarked is the marker of alwaysUntrusted: markAlways, and the answer
// a CLI command shows at its gate (Caller.ask), so the two are one.
var alwaysMarked = untrustedAnswer{fields: alwaysUntrusted, deep: true}

// markUntrusted adds "_untrusted" to the answers of a tool that declares
// them (Decl.Untrusted): the fields of u the answer has, beside those the
// answer marks itself (an error's, errAnswer). The call carries u
// (markerOf): the answer a CLI command shows at its gate is marked the
// same (Caller.ask).
func markUntrusted(u untrustedAnswer, next Handler) Handler {
	return func(ctx context.Context, args map[string]any) (string, bool) {
		text, isErr := next(context.WithValue(ctx, markerKey{}, u), args)
		if u.whole && isErr {
			return text, isErr
		}
		return withUntrusted(u, text), isErr
	}
}

// withUntrusted is answer text with the fields of u it has added to its
// "_untrusted"; text as it is when it has none or is not an object.
func withUntrusted(u untrustedAnswer, text string) string {
	dec := json.NewDecoder(bytes.NewReader([]byte(text)))
	dec.UseNumber()
	var m map[string]any
	if dec.Decode(&m) != nil {
		return text
	}
	has := u.in(m)
	if len(has) == 0 {
		return text
	}
	for _, f := range stringList(m["_untrusted"]) {
		if !slices.Contains(has, f) {
			has = append(has, f)
		}
	}
	m["_untrusted"] = has
	return toJSON(m)
}
