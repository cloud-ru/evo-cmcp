// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"errors"
	"net"
	"syscall"

	"github.com/cloud-ru/evo-cmcp/internal/i18n"
)

// sshProbeTimeout bounds the probe of port 22 before the question: a share
// of a preview's lookups (PreviewLookupTimeout), long enough for a VM
// across the internet, so the preview does not wait long on a host whose
// port 22 out is closed.
const sshProbeTimeout = PreviewLookupTimeout / 4

func SSHUpload(ip string) string {
	if ip == "" {
		return i18n.T("over SSH from this host: needs outbound TCP 22 to the VM's public address",
			"по SSH с этого хоста: нужен исходящий TCP 22 к публичному адресу ВМ")
	}
	return i18n.Tf("over SSH from this host: needs outbound TCP 22 to %s",
		"по SSH с этого хоста: нужен исходящий TCP 22 к %s", ip)
}

func (e *Engine) ReachSSH(ctx context.Context, ip string) (probed bool, err error) {
	if ip == "" || e.SSHDial == nil {
		return false, nil
	}
	if err, ok := e.sshReached[ip]; ok {
		return true, err
	}
	pctx, cancel := context.WithTimeout(ctx, sshProbeTimeout)
	defer cancel()
	err = e.SSHDial(pctx, net.JoinHostPort(ip, "22"))
	if e.sshReached == nil {
		e.sshReached = map[string]error{}
	}
	e.sshReached[ip] = err
	return true, err
}

// DialSSH is the SSHDial of the CLI and the MCP server: a TCP connect to
// addr, closed at once — no byte sent, no key, no banner read.
func DialSSH(ctx context.Context, addr string) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err == nil {
		conn.Close()
	}
	return err
}

// SSHProbeClass is the class of a failed probe for the answer
// (ssh_probe_error): timeout, refused (nothing listens on 22), unreachable
// (no route), or error.
func SSHProbeClass(err error) string {
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()):
		return "timeout"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH):
		return "unreachable"
	}
	return "error"
}

// SSHUnreachable is the warning of a probe that failed: the update will
// wait for SSH after consent and likely fail. A warning, not a refusal:
// the VM may still be booting, and the person decides.
func SSHUnreachable(ip string, err error) string {
	return i18n.Tf("TCP 22 of %s does not answer from this host (%s): the update uploads over SSH and will likely fail. "+
		"Check with nc -vz %s 22; outbound port 22 may be closed in this network (a firewall, a VPN, an agent's sandbox), "+
		"or the VM is stopped or still booting (vm_get)",
		"TCP 22 у %s не отвечает с этого хоста (%s): обновление загружает файлы по SSH и, скорее всего, не пройдёт. "+
			"Проверка — nc -vz %s 22; исходящий порт 22 может быть закрыт в этой сети (межсетевой экран, VPN, песочница агента), "+
			"или ВМ остановлена либо ещё загружается (evo vm get)", ip, SSHProbeClass(err), ip)
}
