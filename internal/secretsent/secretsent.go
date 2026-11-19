// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package secretsent

import (
	"context"
	"sync/atomic"
)

type key struct{}

// flag is the mark of one call and of the calls it is made in.
type flag struct {
	set    atomic.Bool
	parent *flag
}

// Watch is ctx with a mark of its own for the call it carries; a mark set
// in it is set in the marks of the calls the call is made in too (a step of
// a stack: the stack's answer carries the step's error).
func Watch(ctx context.Context) context.Context {
	parent, _ := ctx.Value(key{}).(*flag)
	return context.WithValue(ctx, key{}, &flag{parent: parent})
}

// Mark marks the call of ctx, and the calls it is made in, as one that sent
// a value that may be a secret. A ctx no call watches is not marked.
func Mark(ctx context.Context) {
	for f, _ := ctx.Value(key{}).(*flag); f != nil; f = f.parent {
		f.set.Store(true)
	}
}

// Sent reports whether the call of ctx sent a value that may be a secret.
func Sent(ctx context.Context) bool {
	f, _ := ctx.Value(key{}).(*flag)
	return f != nil && f.set.Load()
}
