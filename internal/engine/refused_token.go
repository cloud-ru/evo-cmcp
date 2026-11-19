// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package engine

import clerrors "github.com/cloud-ru/evo-cmcp/internal/errors"

type refusals struct{ last bool }

// keyRefused records the result of a request and reports whether the key
// is refused: err is a refused token and so was the one before.
func (r *refusals) keyRefused(err error) bool {
	refused := refusedToken(err)
	again := refused && r.last
	r.last = refused
	return again
}

// refusedToken: the service refused the token (401) or the key.
func refusedToken(err error) bool {
	reason := clerrors.Reason(err)
	return err != nil && (reason == clerrors.CodeAuthExpired || reason == clerrors.CodeAuthFailed)
}
