// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package tools

import (
	"context"
	stderrors "errors"
	"fmt"
	"maps"

	"github.com/cloud-ru/evo-cmcp/internal/engine"
	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
	"github.com/cloud-ru/evo-cmcp/internal/provision"
	"github.com/cloud-ru/evo-cmcp/internal/state"
)

func creatingVM(ctx context.Context, eng *engine.Engine, name string, drop bool) (*state.DeploymentRecord, *provision.VM, error) {
	if name == "" {
		return nil, nil, nil
	}
	path := state.DefaultPath(eng.StateDir)
	sf, err := state.Read(path)
	if err != nil {
		return nil, nil, nil
	}
	var rec *state.DeploymentRecord
	for _, d := range sf.ActiveDeployments() {
		if d.Source == state.SourceProvision && d.Status == "creating" && d.VMName == name &&
			d.ProjectID == eng.Config.ProjectID && d.Region == eng.Config.Region {
			rec = d
		}
	}
	if rec == nil {
		return nil, nil, nil
	}
	vm, err := eng.Cloud.GetVM(ctx, rec.VMID)
	var ce *clierrors.CLIError
	switch {
	case stderrors.As(err, &ce) && ce.Code == clierrors.CodeVMNotFound:
		if drop {
			_ = state.Update(path, stateLockWait, func(sf *state.File) error {
				sf.RemoveDeployment(rec.ID)
				return nil
			})
		}
		return nil, nil, nil
	case err != nil:
		return nil, nil, fmt.Errorf(i18n.T("VM %s, which an earlier provision of %s left creating: %w",
			"ВМ %s, которую прошлый provision с именем %s оставил создаваться: %w"), rec.VMID, name, err)
	}
	return rec, vm, nil
}

func provisionCreating(args map[string]any, vmID, vmName, vmStatus, password string, elapsedSec int, warning string) map[string]any {
	repeat := maps.Clone(args)
	delete(repeat, "wait")
	delete(repeat, "image_metadata")
	repeat["name"] = vmName
	repeat["confirmed"] = true
	repeat[continuedVMArg] = vmID
	out := map[string]any{
		"status":      "creating",
		"vm_id":       vmID,
		"vm_status":   vmStatus,
		"name":        vmName,
		"elapsed_sec": elapsedSec,
		"next": i18n.Tf("The VM is still being created. Call provision again with next_args (name %s, confirmed=true, vm_id) from the same folder: it adopts this VM (no second one), waits and finishes it (public IP, ports); if the VM is gone by then, it refuses and creates nothing. vm_get vm_id=%s shows its state.%s",
			"ВМ ещё создаётся. Повторите evo vm create %s с --vm-id %[2]s из той же папки: команда продолжит эту ВМ (вторую не создаст), дождётся её и доделает (публичный IP, порты); если ВМ к тому времени исчезнет — откажет и ничего не создаст. Состояние показывает evo vm get %[2]s.%[3]s",
			vmName, vmID, warning),
	}
	nextStep(out, "provision", repeat)
	if password != "" {
		out["password"] = password
		out["next"] = out["next"].(string) + passwordOnce()
	}
	return out
}

const continuedVMArg = "vm_id"

// continuedVMDesc is the description of continuedVMArg in a schema.
const continuedVMDesc = "next_args: its VM"

func (s *Registry) continuedVM(ctx context.Context, tool string, args map[string]any, name string) error {
	id := stringArg(args, continuedVMArg)
	if id == "" {
		return nil
	}
	eng, err := s.authedEngine(ctx, nil)
	if err != nil {
		return err
	}
	vm, err := eng.Cloud.GetVM(ctx, id)
	var ce *clierrors.CLIError
	if stderrors.As(err, &ce) && ce.Code == clierrors.CodeVMNotFound {
		return clierrors.New(clierrors.CodeVMNotFound,
			i18n.Tf("the VM this call went on with (%s) is no longer there; no new VM was created", "ВМ, которую продолжал вызов (%s), больше нет; новая не создана", id),
			i18n.Tf("A new VM needs a new consent: ask the person; a call of %[1]s without %[2]s and without confirmed shows its preview. The call that was cut may have left a floating IP and the folder's record of that VM: whether to remove them (destroy vm_id=%[3]s from the same folder) — ask the person.",
				"Новой ВМ нужно новое согласие: спросите человека; вызов %[1]s без %[2]s и без confirmed покажет превью. Оборванный вызов мог оставить floating IP и запись этой ВМ в папке: убирать ли их (destroy vm_id=%[3]s из той же папки) — спросите человека.",
				tool, continuedVMArg, id))
	}
	if err != nil {
		return err
	}
	if name != "" && vm.Name != "" && vm.Name != name {
		return clierrors.New(clierrors.CodeUsage,
			i18n.Tf("%s %s is VM %q, and the call names %q: the step goes on with the VM it was given, under its name", "%s %s — это ВМ %q, а вызов называет %q: шаг продолжает ту ВМ, что ему дана, под её именем", continuedVMArg, id, vm.Name, name),
			i18n.T("Call the step with next_args as the answer gave them.", "Вызовите шаг с next_args, как их дал ответ."))
	}
	return nil
}
