// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"

	"github.com/cloud-ru/evo-cmcp/internal/i18n"
	"github.com/cloud-ru/evo-cmcp/internal/output"
)

func renderVMCreated(p *output.Printer, w io.Writer, text string) error {
	var m map[string]any
	if err := json.Unmarshal([]byte(text), &m); err != nil {
		return renderAnswer(p, w, text, tableView{})
	}
	ssh, _ := m["ssh"].(string)
	password, _ := m["password"].(string)
	stateDir, _ := m["state_dir"].(string)
	for k, v := range map[string]string{"ssh": ssh, "password": password, "state_dir": stateDir} {
		if v != "" {
			delete(m, k) // shown below the table
		}
	}
	rest, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := renderAnswer(p, w, string(rest), tableView{}); err != nil {
		return err
	}
	if ssh != "" {
		fmt.Fprintln(w, "SSH: "+ssh)
	}
	if password != "" {
		fmt.Fprintln(w, vmPasswordLine(password))
	}
	if stateDir != "" {
		fmt.Fprintln(w, i18n.Tf("The SSH key and the state are in %s: run evo status and evo vm delete from %s.",
			"Ключ SSH и state — в %s: evo status и evo vm delete запускайте из %s.", stateDir, filepath.Dir(stateDir)))
	}
	return nil
}

// vmPasswordLine shows the password evo generated for a VM: in the answer
// of evo vm create, and in its error when the call left the VM coming up
// (Ctrl-C, the timeout).
func vmPasswordLine(password string) string {
	return i18n.Tf("VM password: %s (shown only here — save it: evo keeps no copy)",
		"Пароль ВМ: %s (записан только здесь — сохраните: evo его не хранит)", password)
}
