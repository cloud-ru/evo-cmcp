// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"slices"
	"strings"

	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
)

func (z *AvailabilityZone) Closed() bool {
	return z != nil && z.Enabled != nil && !*z.Enabled
}

// ClosedZoneError refuses a VM in the zone zoneID when the zone list marks
// it closed — before anything is created or billed. nil when the zone is
// open, unknown or the list is empty.
func ClosedZoneError(zones []*AvailabilityZone, zoneID string) error {
	var zone *AvailabilityZone
	var open []string
	for _, z := range zones {
		if z.ID == zoneID {
			zone = z
		} else if !z.Closed() && z.Name != "" {
			open = append(open, z.Name)
		}
	}
	if !zone.Closed() {
		return nil
	}
	hint := i18n.T("Pick another zone with 'evo init', or pass its ID from 'evo list-zones' as --region.",
		"Выберите другую зону: 'evo init' или --region с её ID из 'evo list-zones'.")
	if len(open) > 0 {
		hint = i18n.Tf("Pick an open zone (%s) with 'evo init', or pass its ID from 'evo list-zones' as --region.",
			"Выберите открытую зону (%s): 'evo init' или --region с её ID из 'evo list-zones'.", strings.Join(open, ", "))
	}
	return clierrors.New(clierrors.CodeConfigInvalid,
		i18n.Tf("zone %s is closed for new resources (the zone list marks it enabled=false); nothing was created",
			"зона %s закрыта для новых ресурсов (в списке зон enabled=false); ничего не создано", zone.Name),
		hint)
}

// OpenZones drops the closed zones, keeping the order; all of them when
// every zone is closed (then the create call says why).
func OpenZones(zones []*AvailabilityZone) []*AvailabilityZone {
	out := make([]*AvailabilityZone, 0, len(zones))
	for _, z := range zones {
		if !z.Closed() {
			out = append(out, z)
		}
	}
	if len(out) == 0 {
		return zones
	}
	return out
}

// ResolveZone is the listed zone ref names: by its UUID or by its name
// ("ru.AZ-2", in any case). It is the one rule for a zone a person or an
// agent names: the zone of a new VM (--region of evo vm create, the region
// of MCP provision, CLOUDRU_REGION), evo configure --region, a stack's
// vm.zone and the zone of a Kafka or Redis cluster. A ref no listed zone
// matches is USAGE with the names to choose from (the open zones).
func ResolveZone(zones []*AvailabilityZone, ref string) (*AvailabilityZone, error) {
	for _, z := range zones {
		if z.ID == ref {
			return z, nil
		}
	}
	for _, z := range zones {
		if z.Name != "" && strings.EqualFold(z.Name, ref) {
			return z, nil
		}
	}
	var names []string
	for _, z := range OpenZones(zones) {
		if z.Name != "" {
			names = append(names, z.Name)
		}
	}
	hint := i18n.T("The zones are listed by evo vm zones (list_zones).", "Зоны — в выводе evo vm zones (list_zones).")
	if len(names) > 0 {
		hint = i18n.Tf("Zones: %s — a name or its UUID (evo vm zones, list_zones).",
			"Зоны: %s — имя или UUID (evo vm zones, list_zones).", strings.Join(names, ", "))
	}
	return nil, clierrors.New(clierrors.CodeUsage,
		i18n.Tf("availability zone %q not found", "зона доступности %q не найдена", ref), hint)
}

func FlavorOutOfZone(f *Flavor, zoneID, zoneName string) error {
	names := f.ZoneNames()
	if len(names) == 0 || (zoneID == "" && zoneName == "") || AvailableIn(f, zoneID, zoneName) {
		return nil
	}
	where := zoneName
	if where == "" {
		where = zoneID
	}
	return clierrors.New(clierrors.CodeUsage,
		i18n.Tf("flavor %s is not available in zone %s: a VM of it can be created in %s",
			"flavor %s недоступен в зоне %s: ВМ с ним создаётся в %s", f.Name, where, strings.Join(names, ", ")),
		i18n.Tf("Pick a flavor of zone %s (list_flavors zone=%s, 'evo vm flavors --zone %s') or one of the flavor's zones.",
			"Выберите flavor зоны %s (list_flavors zone=%s, 'evo vm flavors --zone %s') или одну из зон flavor.", where, where, where))
}

func AvailableIn(f *Flavor, zoneID, zoneName string) bool {
	names := f.ZoneNames()
	for _, z := range f.Zones {
		if (zoneID != "" && z.ID == zoneID) || (zoneName != "" && strings.EqualFold(z.Name, zoneName)) {
			if slices.Contains(names, z.Name) {
				return true
			}
		}
	}
	return false
}

func FlavorsInZone(flavors []*Flavor, zoneID, zoneName string) []*Flavor {
	var out []*Flavor
	for _, f := range flavors {
		if FlavorOutOfZone(f, zoneID, zoneName) == nil {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		return flavors
	}
	return out
}
