// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"os"
	"path/filepath"

	clerrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
	"github.com/cloud-ru/evo-cmcp/internal/state"
)

// composePath is the absolute path of a compose file, as a deployment
// records it (state.DeploymentRecord.ComposePath).
func composePath(file string) string {
	if abs, err := filepath.Abs(file); err == nil {
		return abs
	}
	return filepath.Clean(file)
}

func sameComposeFile(recorded, path string) bool {
	if recorded == path {
		return true
	}
	a, errA := os.Stat(recorded)
	b, errB := os.Stat(path)
	return errA == nil && errB == nil && os.SameFile(a, b)
}

func composeRefusal(rec *state.DeploymentRecord, file string) error {
	if rec == nil || rec.ComposePath == "" || sameComposeFile(rec.ComposePath, composePath(file)) {
		return nil
	}
	return clerrors.New(clerrors.CodeUsage,
		i18n.Tf("deployment %s runs %s, not %s: deploy would send another project's compose file to its VM; nothing was done",
			"развёртывание %s работает с %s, а не с %s: deploy отправил бы на его ВМ compose-файл другого проекта; ничего не сделано",
			rec.ID, rec.ComposePath, composePath(file)),
		i18n.Tf("Deploy from the directory of %s (for evo mcp: start the server there), or pass that file as compose_file.",
			"Запустите deploy из каталога %s (для evo mcp — запустите сервер там) или передайте этот файл в compose_file.", rec.ComposePath))
}

// composeUnknown is the warning of a deploy that updates the VM of rec
// whose record names no compose file (from before 1.6): which project it
// runs is not known, and this deploy sends file to it.
func composeUnknown(rec *state.DeploymentRecord, file string) string {
	if rec == nil || rec.ComposePath != "" {
		return ""
	}
	return i18n.Tf("deployment %s does not record its compose file (made before evo 1.6): this deploy sends %s to its VM and records it",
		"развёртывание %s не записало свой compose-файл (создано до evo 1.6): этот deploy отправит на его ВМ %s и запишет его",
		rec.ID, composePath(file))
}

// DeployComposeCheck is composeRefusal and composeUnknown of the deployment
// this directory's deploy would update, for the deploy tool's answer
// before the question: the refusal, else the warning ("" without one).
func (e *Engine) DeployComposeCheck() (warning string, err error) {
	sf, rerr := state.Read(state.DefaultPath(e.StateDir))
	if rerr != nil {
		return "", nil
	}
	rec := sf.ActiveComposeDeployment()
	if err := composeRefusal(rec, e.Config.ComposeFile); err != nil {
		return "", err
	}
	return composeUnknown(rec, e.Config.ComposeFile), nil
}
