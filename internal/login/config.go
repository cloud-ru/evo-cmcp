// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cloud-ru/evo-cmcp/internal/auth"
	"github.com/cloud-ru/evo-cmcp/internal/clock"
	"github.com/cloud-ru/evo-cmcp/internal/config"
	"github.com/cloud-ru/evo-cmcp/internal/httpretry"
)

type Config struct {
	// Issuer is the OpenID Connect issuer; Trusted checks every address the
	// discovery document names (auth.TrustedOIDCURL).
	Issuer  string
	Trusted func(string) bool
	// OrgBase and AdminBase are the bases of the Organization and
	// Administration APIs; KeyToken is IAM's token exchange of an access key.
	OrgBase, AdminBase, KeyToken string
	HTTP                         *http.Client
	// Ports are the loopback ports of the registered redirect URIs.
	Ports []int
	// Open opens the URL in the person's browser; an error is no failure
	// of the login: the URL is printed too.
	Open func(url string) error
	// Say tells the person, in the terminal, the address to open.
	Say func(message, url string)
	// Tell writes a line for the person in the terminal (stderr): the
	// wizard, the summary and the steps.
	Tell func(line string)
	// Wait bounds the time from the start to the person's choice.
	Wait time.Duration
	// LockPath is the lock of the one evo login of this computer; "" takes
	// none.
	LockPath string
	// Now is the clock of the answer's values (key's term, login date).
	Now       func() time.Time
	IntentDir string
	// Remote: the terminal is over SSH, so the person's browser is likely
	// on another computer and cannot reach this one's 127.0.0.1 (P1).
	Remote bool
}

// Default is the production Config.
func Default(say func(message, url string), open func(string) error) Config {
	return Config{
		Issuer: auth.OIDCIssuer, Trusted: auth.TrustedOIDCURL,
		OrgBase: "https://organization.api.cloud.ru", AdminBase: "", KeyToken: auth.IAMEndpoint,
		HTTP:  &http.Client{Timeout: 30 * time.Second, Transport: httpretry.UserAgent(nil), CheckRedirect: noRedirects},
		Ports: auth.LoginRedirectPorts[:], Open: open, Say: say,
		Wait:     5 * time.Minute,
		Now:      clock.Now,
		LockPath: filepath.Join(config.Dir(), "login.lock"),
		Remote:   os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "",
	}
}

// noRedirects: a request with the person's code or token is never sent on
// to wherever a Location points.
func noRedirects(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
