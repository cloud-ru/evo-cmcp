// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/cloud-ru/evo-cmcp/internal/auth"
	"github.com/cloud-ru/evo-cmcp/internal/config"
	"github.com/cloud-ru/evo-cmcp/internal/confirm"
	"github.com/cloud-ru/evo-cmcp/internal/deploy"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
	"github.com/cloud-ru/evo-cmcp/internal/output"
	"github.com/cloud-ru/evo-cmcp/internal/provision"
	"github.com/cloud-ru/evo-cmcp/internal/sgrules"
	"github.com/cloud-ru/evo-cmcp/internal/state"
	"github.com/cloud-ru/evo-cmcp/internal/verify"
	"github.com/cloud-ru/evo-cmcp/internal/vmstage"
)

// Engine orchestrates all CLI operations.
type Engine struct {
	Config    config.Config
	Auth      auth.TokenProvider
	Cloud     provision.CloudAPI
	Deployer  deploy.Deployer
	Printer   *output.Printer
	SSHKeyPEM []byte // Ed25519 private key for SSH
	SSHPubKey string // Ed25519 public key for image metadata
	StateDir  string // project root (for state file)

	// SSHKeyLoader loads or creates the key pair on first use (EnsureSSHKey),
	// so commands that never reach a VM leave no key behind.
	SSHKeyLoader func() (privPEM []byte, pubKey string, err error)

	// Timings overrides poll/retry waits; zero fields keep production defaults.
	Timings Timings

	QuotaProbe func(ctx context.Context, tp auth.TokenProvider, projectID string) error
	// Probe runs post-deploy health checks; nil means verify.Check.
	Probe func(ctx context.Context, ip string, ports []int, timeout time.Duration) []verify.ProbeResult
	// SSHDial probes port 22 of a VM before the question of an update
	// (ReachSSH, M2): DialSSH in the CLI and the server; nil, no probe.
	SSHDial func(ctx context.Context, addr string) error
	// sshReached remembers ReachSSH per address within one call.
	sshReached map[string]error
	// zones is the zone list of the call, once read (zoneList).
	zones []*provision.AvailabilityZone
	// Stages runs the long stages of deploy on the VM (vmstage); nil: the
	// Deployer, when it runs stages itself, else vmstage.Over(Deployer).
	Stages vmstage.Runner

	// until is DeployOptions.Until of the deploy that runs.
	until time.Time
	// boundFrom is when that deploy began: the bound's length is
	// until - boundFrom.
	boundFrom time.Time
	// phase is the step of the deploy that runs, as InProgress names it:
	// a step cut by the bound is going on from there (atBound).
	phase string
	// portsOnlyFromGroups are the ports the last openPorts found let in
	// only from other security groups: the answer of deploy names them.
	portsOnlyFromGroups []sgrules.GroupOnlyPort
	// now is the clock of the bound (past); nil: time.Now. A test brings
	// the bound at a point of the deploy with it, not by the wall clock.
	now func() time.Time

	decided bool
}

// DeployResult identifies the VM this call created, updated or found unchanged.
type DeployResult struct {
	Record state.DeploymentRecord
	Action string
}

// DeployOptions holds parameters for the deploy command.
type DeployOptions struct {
	// Result receives the outcome selected under the state lock.
	Result         *DeployResult
	ComposeFile    string
	Flavor         string // "auto" or explicit flavor name
	Region         string
	Force          bool // force redeploy even if hash unchanged
	NonInteractive bool // never prompt; creating a VM then needs Yes
	Yes            bool // consent to create the VM given in advance (--yes)
	DryRun         bool // show what would be created and its cost, then stop
	// Cidr is the source the rules of the ports deploy opens let in
	// (sgrules.Source: 0.0.0.0/0 when empty, P5); rules already there
	// stay as they are.
	Cidr string
	// Gate, when set, asks for consent where Deploy would call confirm.Ask
	// with its own preview: the deploy tool sets it for 'evo deploy', which
	// shows the preview of the tool's answer (spec REQ-4, REQ-6). redeploy
	// is the engine's words for an update of the existing VM, nil for a new
	// VM, whose preview the tool makes itself.
	Gate func(ctx context.Context, redeploy *confirm.Preview) (bool, error)

	Until time.Time
}

// EnsureSSHKey fills SSHKeyPEM/SSHPubKey from SSHKeyLoader unless they are
// already set. Commands that connect to the VM call it before they need the key.
func (e *Engine) EnsureSSHKey() error {
	if len(e.SSHKeyPEM) > 0 || e.SSHKeyLoader == nil {
		return nil
	}
	priv, pub, err := e.SSHKeyLoader()
	if err != nil {
		return fmt.Errorf(i18n.T("SSH key: %w", "SSH-ключ: %w"), err)
	}
	e.SSHKeyPEM, e.SSHPubKey = priv, pub
	return nil
}
