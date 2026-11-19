// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package sgrules

import (
	"net/netip"
	"sort"

	"github.com/cloud-ru/evo-cmcp/internal/provision"
)

const (
	internetShorterThanV4 = 8
	internetShorterThanV6 = 32
)

// privateRanges are the ranges that are not the internet however wide: a
// prefix that lies wholly in one of them is a private source.
var privateRanges = func() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range []string{
		"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", // RFC 1918
		"100.64.0.0/10",                 // RFC 6598
		"127.0.0.0/8", "169.254.0.0/16", // loopback, link-local
		"fc00::/7", "fe80::/10", "::1/128",
	} {
		out = append(out, netip.MustParsePrefix(s))
	}
	return out
}()

// v4InV6 is the range of IPv4 addresses written as IPv6 (::ffff:a.b.c.d).
var v4InV6 = netip.MustParsePrefix("::ffff:0:0/96")

func Internet(cidr string) bool {
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		return false
	}
	if p.Addr().Is6() {
		switch {
		case p.Bits() >= v4InV6.Bits() && v4InV6.Contains(p.Addr()):
			p = netip.PrefixFrom(p.Addr().Unmap(), p.Bits()-v4InV6.Bits())
		case p.Bits() < v4InV6.Bits() && p.Contains(v4InV6.Addr()):
			return true
		}
	}
	p = p.Masked()
	limit := internetShorterThanV4
	if p.Addr().Is6() {
		limit = internetShorterThanV6
	}
	if p.Bits() >= limit {
		return false
	}
	for _, private := range privateRanges {
		if private.Bits() <= p.Bits() && private.Contains(p.Addr()) {
			return false
		}
	}
	return true
}

// GroupUse is who a rule of a group acts on: whether the group is shared,
// and the interfaces and the VMs under it as far as evo read them.
type GroupUse struct {
	Shared bool
	// Counted is false when the list of interfaces was not read:
	// Interfaces and VMs say nothing then.
	Counted    bool
	Interfaces int
	// VMs are the names (ids, when unnamed) of the VMs under the group,
	// sorted, at most MaxVMsNamed; MoreVMs is how many more there are.
	VMs     []string
	MoreVMs int
}

// MaxVMsNamed is how many VMs under a group a preview names.
const MaxVMsNamed = 10

// Use is the GroupUse of group by the project's interfaces; read is false
// when they could not be read.
func Use(group *provision.SecurityGroup, interfaces []*provision.Interface, read bool) GroupUse {
	if group == nil {
		return GroupUse{}
	}
	if !read {
		return GroupUse{Shared: group.Default || group.HasInterfaces}
	}
	u := GroupUse{Counted: true}
	vms := map[string]bool{}
	for _, in := range interfaces {
		if in == nil {
			continue
		}
		for _, g := range in.SecurityGroups {
			if g.ID != group.ID {
				continue
			}
			u.Interfaces++
			if in.VM != nil {
				name := in.VM.Name
				if name == "" {
					name = in.VM.ID
				}
				if name != "" {
					vms[name] = true
				}
			}
			break
		}
	}
	for name := range vms {
		u.VMs = append(u.VMs, name)
	}
	sort.Strings(u.VMs)
	if len(u.VMs) > MaxVMsNamed {
		u.MoreVMs = len(u.VMs) - MaxVMsNamed
		u.VMs = u.VMs[:MaxVMsNamed]
	}
	u.Shared = group.Default || u.Interfaces > 1
	return u
}

// OpenToInternet reports whether r lets the internet in now: an active
// ingress rule whose source is a prefix that is the internet (Internet).
// A rule from a group lets in that group's interfaces; a rule that names
// no source is not judged — what the platform answers for «from anywhere»
// is not verified.
func OpenToInternet(r *provision.SGRule) bool {
	return r != nil && r.Direction == "ingress" && !inactive[r.State] &&
		r.RemoteSecurityGroup == nil && r.RemoteIPPrefix != "" && Internet(r.RemoteIPPrefix)
}
