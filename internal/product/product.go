// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

// Package product holds the product's display name and authorship. The
// binary, the MCP server name, the environment variables and the config
// paths stay "evo".
package product

import "sync/atomic"

const Name = "evo CMCP"

const PageName = "Evolution CMCP"

const (
	TitleEN = Name + " — CLI and MCP server for Cloud.ru Evolution"
	TitleRU = Name + " — CLI и MCP-сервер для Cloud.ru Evolution"
)

const FeedbackContact = "https://github.com/cloud-ru/evo-cmcp/issues"

// Author is who made evo. It is in evo version, the hidden "evo crew",
// NOTICE (Apache 2.0, section 4(d): a redistribution keeps NOTICE) and the
// SPDX header of every Go file; TestAuthorship keeps them in step.
const (
	Author   = "Cloud.ru"
	AuthorEN = "Cloud.ru"
	Repo     = "https://github.com/cloud-ru/evo-cmcp"
)

var version atomic.Value // string

// SetVersion records the build version for UserAgent; the CLI sets it at
// start.
func SetVersion(v string) { version.Store(v) }

// UserAgent is the User-Agent of evo's requests to Cloud.ru: the product
// and where it comes from, so the platform side can tell evo traffic (and
// a copy of its clients) from the rest.
func UserAgent() string {
	v, _ := version.Load().(string)
	if v == "" {
		v = "dev"
	}
	return "evo/" + v + " (" + Name + "; +" + Repo + ")"
}
