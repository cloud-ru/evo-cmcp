// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/cloud-ru/evo-cmcp/internal/auth"
	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/iamadmin"
)

// The statuses of the key in the answer of logout.
const (
	KeyDeleted        = "deleted"
	KeyAlreadyDeleted = "already_deleted"
	KeyRejected       = "rejected"
)

// LogoutResult is what logout did to the key.
type LogoutResult struct {
	Key string
	// TokensUntil: the tokens the key was exchanged for before still work
	// until then at the latest — IAM does not revoke them with the key.
	TokensUntil time.Time
}

func Logout(ctx context.Context, adminBase string, key auth.TokenProvider, keyID, serviceAccountID, accessKeyID string) (LogoutResult, error) {
	// The exchange first: a key IAM refuses is not a failure to delete it.
	tok, err := key.Token(ctx)
	var ce *clierrors.CLIError
	if errors.As(err, &ce) && ce.Code == clierrors.CodeAuthFailed {
		return LogoutResult{Key: KeyRejected}, nil
	}
	if err == nil {
		err = iamadmin.New(key, adminBase).DeleteAccessKey(ctx, serviceAccountID, accessKeyID)
	}
	switch {
	case err == nil:
		// A fresh token lives the longest: the ones before end by then.
		return LogoutResult{Key: KeyDeleted, TokensUntil: tok.ExpiresAt}, nil
	case iamadmin.StatusOf(err) == http.StatusNotFound:
		return LogoutResult{Key: KeyAlreadyDeleted}, nil
	}
	return LogoutResult{}, clierrors.Wrap(clierrors.CodeAPIError,
		eKeyNotDeleted(keyID, accessKeyID, err),
		eKeyNotDeletedHint(), err)
}
