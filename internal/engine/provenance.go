// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"time"

	clerrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
	"github.com/cloud-ru/evo-cmcp/internal/provision"
	"github.com/cloud-ru/evo-cmcp/internal/state"
)

// The answers of Provenance.CreatedByEvo.
const (
	ProvenanceYes     = "yes"
	ProvenanceNo      = "no"
	ProvenanceUnknown = "unknown"
)

const (
	ProvenanceVM         = "vm"
	ProvenanceFloatingIP = "floating_ip"
)

type Provenance struct {
	CreatedByEvo string `json:"created_by_evo"`
	Note         string `json:"note"`
}

// recordedID are the fields of a record that name a resource of a kind the
// state records.
var recordedID = map[string]func(state.DeploymentRecord) string{
	ProvenanceVM:         func(r state.DeploymentRecord) string { return r.VMID },
	ProvenanceFloatingIP: func(r state.DeploymentRecord) string { return r.FloatingIPID },
}

func ProvenanceOf(kind, id string, made time.Time, madeErr error, sf *state.File, readErr error) Provenance {
	unknown := func(note string) Provenance { return Provenance{CreatedByEvo: ProvenanceUnknown, Note: note} }
	idOf, recorded := recordedID[kind]
	switch {
	case !recorded:
		return unknown(i18n.T("evo does not record this kind of resource until the journal of evo 1.8: whether evo made it is unknown",
			"evo не записывает такие ресурсы до журнала evo 1.8: создан ли он evo — неизвестно"))
	case readErr != nil:
		return unknown(i18n.Tf("this folder's state could not be read (%s): evo cannot tell who made it",
			"state этой папки не прочитан (%s): кто создал ресурс, evo сказать не может", clerrors.Reason(readErr)))
	}
	var own, adopted *state.DeploymentRecord
	if sf != nil && id != "" {
		for i := range sf.Deployments {
			rec := &sf.Deployments[i]
			if idOf(*rec) != id {
				continue
			}
			if madeBeforeRecord(*rec, made) {
				adopted = rec
			} else if own == nil {
				own = rec
			}
		}
	}
	switch {
	case (adopted != nil || own != nil) && madeErr != nil:
		return unknown(i18n.Tf("this folder's state records it, but the cloud's time it was made could not be read (%s): whether evo made it is unknown",
			"state этой папки его записывает, но время создания из облака не прочитано (%s): создан ли он evo — неизвестно", clerrors.Reason(madeErr)))
	case adopted != nil:
		// Of several records, one of an adoption decides: destroy refuses
		// it all the same (AdoptedVM).
		return Provenance{CreatedByEvo: ProvenanceNo, Note: i18n.Tf(
			"this folder's state records it (deployment %s), but the cloud made it before the record: an evo before 1.7 adopted it by its name, it did not create it",
			"state этой папки его записывает (развёртывание %s), но облако создало его раньше записи: evo до 1.7 взял его по имени, а не создал", adopted.ID)}
	case own != nil:
		return Provenance{CreatedByEvo: ProvenanceYes, Note: i18n.Tf(
			"this folder's state records it (deployment %s), made no earlier than the record",
			"state этой папки его записывает (развёртывание %s), создан не раньше записи", own.ID)}
	}
	return unknown(i18n.T("this folder's state does not record it: it is another folder's or made outside evo (the console, another tool), whether evo made it is unknown",
		"state этой папки его не записывает: он другой папки или создан не через evo (консоль, другой инструмент), создан ли он evo — неизвестно"))
}

func madeBeforeRecord(rec state.DeploymentRecord, made time.Time) bool {
	return !rec.CreatedAt.IsZero() && !made.IsZero() && !provision.MadeSince(&provision.VM{CreatedAt: made}, rec.CreatedAt)
}

// ProvenanceLine is the line of a human preview for p.
func ProvenanceLine(p Provenance) string {
	value := p.CreatedByEvo
	switch p.CreatedByEvo {
	case ProvenanceYes:
		value = i18n.T("yes", "да")
	case ProvenanceNo:
		value = i18n.T("no", "нет")
	case ProvenanceUnknown:
		value = i18n.T("unknown", "неизвестно")
	}
	return i18n.T("created by evo: ", "создан evo: ") + value + " — " + p.Note
}
