// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	"slices"

	"github.com/cloud-ru/evo-cmcp/internal/engine"
	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
	"github.com/cloud-ru/evo-cmcp/internal/provision"
	"github.com/cloud-ru/evo-cmcp/internal/state"
)

func folderVMs(eng *engine.Engine) []string {
	sf, err := state.Read(state.DefaultPath(eng.StateDir))
	if err != nil {
		return nil
	}
	var ids []string
	for _, d := range sf.ActiveDeployments() {
		if d.VMID != "" {
			ids = append(ids, d.VMID)
		}
	}
	return ids
}

func vmNameFree(ctx context.Context, eng *engine.Engine, name string, own func(id string) bool) (string, bool) {
	vm, err := engine.ForeignVMNamed(ctx, eng.Cloud, eng.Config.ProjectID, name, own)
	if err != nil {
		return errResult(err), true
	}
	if vm != nil {
		// A VM own does not pass may still be this folder's: provision takes
		// none but one it left creating (agent run of 1.7.0, №10: the
		// refusal said the state did not record a VM it records).
		if slices.Contains(folderVMs(eng), vm.ID) {
			return recordedVMResult(name, vm.ID, vm.Status)
		}
		return foreignVMResult(name, vm.ID, vm.Status)
	}
	return "", false
}

func recordedVMResult(name, id, status string) (string, bool) {
	return errAnswer(clierrors.New(clierrors.CodeUsage,
		i18n.Tf("a VM named %q already exists (id %s, %s): this folder's evo created it, and provision makes a new VM; nothing was created",
			"ВМ с именем %q уже есть (id %s, %s): её создал evo этой папки, а provision создаёт новую ВМ; ничего не создано", name, quoteCloud(id), provision.VMStatusPhrase(status)),
		i18n.Tf("Pick another name (name). That VM is in this folder's state: vm_get vm_id=%s shows it; whether to delete it, ask the person.",
			"Выберите другое имя (name). Та ВМ — в state этой папки: её показывает vm_get vm_id=%s; удалять ли её, спросите человека.", id)),
		map[string]any{"existing_id": id}), true
}

func foreignVMResult(name, id, status string) (string, bool) {
	return errAnswer(clierrors.New(clierrors.CodeUsage,
		i18n.Tf("a VM named %q already exists (id %s, %s), and this folder's evo did not create it (its state does not record it); nothing was created",
			"ВМ с именем %q уже есть (id %s, %s), и её создал не evo этой папки (в её state её нет); ничего не создано", name, quoteCloud(id), provision.VMStatusPhrase(status)),
		i18n.T("Pick another name (name). Whether to delete that VM, ask the person: it may be someone else's.",
			"Выберите другое имя (name). Удалять ли ту ВМ, спросите человека: она может быть чужой.")),
		map[string]any{"existing_id": id}), true
}

// ownIn is the own of vmNameFree for the VMs ids lists.
func ownIn(ids []string) func(string) bool {
	return func(id string) bool { return slices.Contains(ids, id) }
}
