// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"fmt"
	"slices"

	clerrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
	"github.com/cloud-ru/evo-cmcp/internal/provision"
	"github.com/cloud-ru/evo-cmcp/internal/state"
)

func ForeignVMNamed(ctx context.Context, cloud provision.CloudAPI, projectID, name string, own func(id string) bool) (*provision.VM, error) {
	if name == "" || cloud == nil {
		return nil, nil
	}
	vms, err := cloud.ListVMsInProject(ctx, projectID)
	switch reason := clerrors.Reason(err); {
	case err == nil:
	case reason == clerrors.CodeAuthFailed || reason == clerrors.CodeAuthExpired || reason == clerrors.CodeAuthRequired:
		return nil, nil
	default:
		return nil, fmt.Errorf(i18n.T("check the VM name is free: %w", "проверка, что имя ВМ свободно: %w"), err)
	}
	for _, vm := range vms {
		if vm != nil && vm.Name == name && !own(vm.ID) {
			return vm, nil
		}
	}
	return nil, nil
}

// RecordedIn is the own of ForeignVMNamed for the VMs the state sf of a
// folder records.
func RecordedIn(sf *state.File) func(id string) bool {
	var ids []string
	if sf != nil {
		for _, d := range sf.ActiveDeployments() {
			ids = append(ids, d.VMID)
		}
	}
	return func(id string) bool { return slices.Contains(ids, id) }
}

func deployNameHint() string {
	return i18n.T("deploy names its VM evo-<project_name>: set another project_name in .evo.yaml, or deploy from a directory with another name. Whether to delete that VM, ask the person: it may be someone else's.",
		"deploy называет ВМ evo-<project_name>: задайте другой project_name в .evo.yaml или разверните из каталога с другим именем. Удалять ли ту ВМ, спросите человека: она может быть чужой.")
}

func foreignDeployVM(vm *provision.VM) error {
	return &clerrors.CLIError{Code: clerrors.CodeUsage,
		Message: i18n.Tf("a VM named %q already exists (id %q, %s), and this folder's evo did not create it (its state does not record it); nothing was created",
			"ВМ с именем %q уже есть (id %q, %s), и её создал не evo этой папки (в её state её нет); ничего не создано",
			vm.Name, vm.ID, provision.VMStatusPhrase(vm.Status)),
		Hint:    deployNameHint(),
		Details: map[string]any{"existing_id": vm.ID}}
}

// deployNameFree refuses the name of deploy's new VM when another VM of
// the project has it (foreignDeployVM).
func (e *Engine) deployNameFree(ctx context.Context, projectID, name string, sf *state.File) error {
	vm, err := ForeignVMNamed(ctx, e.Cloud, projectID, name, RecordedIn(sf))
	if err != nil {
		return err
	}
	if vm != nil {
		return foreignDeployVM(vm)
	}
	return nil
}
