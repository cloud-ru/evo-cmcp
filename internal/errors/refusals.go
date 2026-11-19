// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package errors

import (
	stderrors "errors"
	"strings"
)

type Refusals []error

func (r Refusals) Error() string {
	msgs := make([]string, len(r))
	for i, e := range r {
		msgs[i] = e.Error()
	}
	return strings.Join(msgs, "; ")
}

func (r Refusals) Unwrap() []error { return r }

// Join is the refusals errs, nil ones dropped: nil for none, the error
// itself for one, Refusals for more.
func Join(errs ...error) error {
	var r Refusals
	for _, e := range errs {
		var more Refusals
		switch {
		case e == nil:
		case stderrors.As(e, &more):
			r = append(r, more...)
		default:
			r = append(r, e)
		}
	}
	switch len(r) {
	case 0:
		return nil
	case 1:
		return r[0]
	}
	return r
}

// All are the refusals err stands for: each of Refusals, else err alone.
func All(err error) []error {
	var r Refusals
	if stderrors.As(err, &r) {
		return r
	}
	return []error{err}
}
