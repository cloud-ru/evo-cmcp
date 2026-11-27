// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"

	clerrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
	"github.com/cloud-ru/evo-cmcp/internal/state"
)

// Maker is the key evo now runs with, as a deployment record keeps it;
// nil without a key.
func (e *Engine) Maker() *state.Maker {
	return state.NewMaker(e.Config.ServiceAccountID, e.Config.KeyID)
}

func (e *Engine) NotVisibleToThisKey(rec state.DeploymentRecord) error {
	if !rec.CreatedBy.Other(e.Maker()) {
		return nil
	}
	return clerrors.New(clerrors.CodeConfigInvalid,
		i18n.Tf("VM %s was recorded by another key (%s), and this key (%s) does not see it: Cloud.ru answers 404 for a VM a key does not see, as for a deleted one; nothing was deleted and its record is kept",
			"ВМ %s записана другим ключом (%s), а этот ключ (%s) ее не видит: Cloud.ru отвечает 404 и на ВМ, которую ключ не видит, и на удаленную; ничего не удалено, запись сохранена",
			rec.VMID, makerName(rec.CreatedBy), makerName(e.Maker())),
		i18n.T("Run it again with the key of the record (its profile or CLOUDRU_KEY_ID), or check the VM in the Cloud.ru console.",
			"Повторите с ключом записи (его профиль или CLOUDRU_KEY_ID) или проверьте ВМ в личном кабинете Cloud.ru."))
}

// makerName names a key by its account, else by its Key ID.
func makerName(m *state.Maker) string {
	if m.ServiceAccountID != "" {
		return i18n.Tf("service account %s", "сервисный аккаунт %s", m.ServiceAccountID)
	}
	return m.KeyID
}

// VisibleToThisKey reads the VM of a record another key made: a 404 is
// NotVisibleToThisKey, before any question or write. Nil for a record of
// this key or of an unknown one, and when the read fails otherwise — the
// caller's own reads decide then.
func (e *Engine) VisibleToThisKey(ctx context.Context, rec state.DeploymentRecord) error {
	if rec.VMID == "" || !rec.CreatedBy.Other(e.Maker()) || e.Cloud == nil {
		return nil
	}
	_, err := e.Cloud.GetVM(ctx, rec.VMID)
	if r := clerrors.Reason(err); r == clerrors.CodeVMNotFound || r == clerrors.CodeNotFound {
		return e.NotVisibleToThisKey(rec)
	}
	return nil
}
