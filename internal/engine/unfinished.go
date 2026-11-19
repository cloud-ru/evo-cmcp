// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	clerrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
	"github.com/cloud-ru/evo-cmcp/internal/state"
)

// unfinishedStatuses are the statuses deploy leaves a record in while it
// works: the VM being created (creating), up with its address (provisioned),
// set up over SSH — Docker, the upload, compose up (deploying).
var unfinishedStatuses = map[string]bool{"creating": true, "provisioned": true, "deploying": true}

type DeployUnfinishedError struct {
	Status string
	err    *clerrors.CLIError
}

func (e *DeployUnfinishedError) Error() string { return e.err.Error() }
func (e *DeployUnfinishedError) Unwrap() error { return e.err }

func deployUnfinished(rec *state.DeploymentRecord) error {
	if rec == nil || !unfinishedStatuses[rec.Status] {
		return nil
	}
	details := map[string]any{"deployment_status": rec.Status}
	tool, args, hint := wayOn(rec)
	if tool != "" {
		details["next_tool"], details["next_args"] = tool, args
	}
	return &DeployUnfinishedError{Status: rec.Status, err: &clerrors.CLIError{
		Code: clerrors.CodeDeployInProgress,
		Message: i18n.Tf("deploy has not finished deployment %s: its record says %s (the VM is being created or set up); its logs and its service are not there yet",
			"deploy ещё не закончил развёртывание %s: в записи — %s (ВМ создаётся или настраивается); журналов и сервиса ещё нет", rec.ID, rec.Status),
		Hint:    hint,
		Details: details,
	}}
}

func wayOn(rec *state.DeploymentRecord) (string, map[string]any, string) {
	switch {
	case rec.Source == state.SourceDeployApply && rec.PlanID != "":
		return "deploy_apply", map[string]any{"plan_id": rec.PlanID, "confirmed": true},
			i18n.Tf("Call deploy_apply again with plan_id %s: it takes the VM of the plan and goes on. status shows how far it got.",
				"Вызовите deploy_apply ещё раз с plan_id %s: он возьмёт ВМ плана и продолжит. status покажет, докуда дошло.", rec.PlanID)
	case rec.Source == "" && rec.ComposePath != "":
		args := map[string]any{"compose_file": rec.ComposePath}
		if rec.ComposeHash != "" {
			args["compose_hash"] = rec.ComposeHash
		}
		hint := i18n.T("Call deploy again with this compose file: it takes the VM in the state and goes on from where it stopped. status shows how far it got.",
			"Вызовите deploy ещё раз с этим compose-файлом: он возьмёт ВМ из state и продолжит с того места, где остановился. status покажет, докуда дошло.")
		if rec.Cidr != "" {
			args["cidr"], args["confirmed"] = rec.Cidr, true
		} else {
			hint += " " + i18n.T("The record does not say which addresses its ports were opened to: the call shows its preview, with cidr, before it goes on.",
				"Запись не говорит, каким адресам открыты её порты: вызов покажет превью с cidr, прежде чем продолжить.")
		}
		return "deploy", args, hint
	case rec.Source == state.SourceProvision:
		return "", nil, i18n.T("This VM is provision's: call provision again with its name, and it takes the VM it left creating. status shows how far it got.",
			"Эта ВМ — от provision: вызовите provision ещё раз с её именем, и он возьмёт ВМ, оставленную в creating. status покажет, докуда дошло.")
	}
	return "", nil, i18n.T("The record does not say which call made it (a record of deploy_apply from before 1.8, or one without its plan): status shows the deployment; go on with the call that made it.",
		"Запись не говорит, каким вызовом она сделана (запись deploy_apply до 1.8 или без плана): status покажет развёртывание; продолжите тем вызовом, который её сделал.")
}
