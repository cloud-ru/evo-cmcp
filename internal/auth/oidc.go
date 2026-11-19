// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"net/url"
	"strings"
)

const (
	// OIDCIssuer is the issuer of Cloud.ru IAM's OpenID Connect: the iss of
	// its tokens and the issuer its discovery document must name.
	OIDCIssuer = "https://id.cloud.ru/auth/system"
	// OIDCDiscoveryURL is the issuer's discovery document (OpenID Connect
	// Discovery 1.0): the login takes its endpoints only from it.
	OIDCDiscoveryURL = OIDCIssuer + "/.well-known/openid-configuration"
	OIDCTokenURL     = "https://id.cloud.ru/auth/system/openid/token"

	// LoginClientID is evo's public client of the login (no secret; PKCE).
	LoginClientID = "evo_cmcp"
	// LoginScope is the one scope the login asks for.
	LoginScope = "openid"

	LoginRoleName = "platform.project.admin"
	// LoginRoleDisplayName is how the console names LoginRoleName: the
	// consent says it to the person, no request needed.
	LoginRoleDisplayName = "Администратор проекта"

	LoginActionCreateAccount = "iam.serviceAccounts.create"
	LoginActionGrantRole     = "iam.permissions.create"
)

// LoginRedirectPorts are the loopback ports of evo_cmcp's three registered
// redirect URIs (http://127.0.0.1:<port>/callback), in the order the login
// tries them.
var LoginRedirectPorts = [...]int{18386, 23386, 28386}

// trustedOIDCHosts are the IAM hosts a secret may be sent to (both answer
// the client-credentials grant, checked 2026-10-14).
var trustedOIDCHosts = map[string]bool{"id.cloud.ru": true, "auth.iam.cloud.ru": true}

// TrustedOIDCURL reports whether u is https on a Cloud.ru IAM host, with
// no user info and no port: the one check before a secret goes to an
// OpenID Connect address that came from outside the table (a kubeconfig,
// a discovery document).
func TrustedOIDCURL(u string) bool {
	p, err := url.Parse(u)
	return err == nil && p.Scheme == "https" && p.User == nil && p.Port() == "" &&
		trustedOIDCHosts[strings.ToLower(p.Hostname())]
}
