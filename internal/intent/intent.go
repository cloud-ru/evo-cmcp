// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package intent

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloud-ru/evo-cmcp/internal/clock"
	"github.com/cloud-ru/evo-cmcp/internal/config"
	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
	"github.com/cloud-ru/evo-cmcp/internal/history"
	"github.com/cloud-ru/evo-cmcp/internal/i18n"
	"github.com/cloud-ru/evo-cmcp/internal/state"
)

func DefiniteRefusal(status int) bool {
	return status >= 400 && status < 500 && status != http.StatusRequestTimeout
}

func OutcomeUnknown(reason string, status int) bool {
	if DefiniteRefusal(status) {
		return false
	}
	if status == http.StatusRequestTimeout {
		return true
	}
	switch reason {
	case clierrors.ReasonInterrupted, clierrors.CodeAPIUnreachable, clierrors.CodeAPIError, clierrors.ReasonGeneral:
		return true
	}
	return false
}

// Intent is one call that went to the cloud and has not answered yet.
type Intent struct {
	ID   string   `json:"id"`
	Tool string   `json:"tool"`
	Args []string `json:"args,omitempty"`
	Name string   `json:"name,omitempty"`
	// Target is the resource a call on an existing one names, as
	// «argument=id» (vm_id=…): a delete, a resize. Not a secret.
	Target    string   `json:"target,omitempty"`
	Parties   []string `json:"parties,omitempty"`
	ProjectID string   `json:"project_id,omitempty"`
	Zone      string   `json:"zone,omitempty"`
	// Key is the key the call was sent with: a read by id answers for the
	// resource only with the key that saw it (state.Maker.Other). Nil in a
	// record of evo before 1.9.
	Key       *state.Maker `json:"key,omitempty"`
	Note      string       `json:"note,omitempty"`
	Surface   string       `json:"surface"`
	PID       int          `json:"pid"`
	StartedAt time.Time    `json:"started_at"`
	Held      bool         `json:"held,omitempty"`
	// Print tells the same call again from another call of the tool on
	// the same resource (a rule of a security group, a kubectl): the hash
	// of the record's id and of the arguments that make the call what it
	// is (Canon). The values are not written — an argument may hold what
	// the file must not; the id in the hash only keeps two records of the
	// same call from showing it by an equal print, it is no defence of a
	// value. "" in a record of evo before the print: such a record is
	// told by its name and target.
	Print string `json:"print,omitempty"`
	// Ended tells a call that returned with the record kept: its outcome
	// is not known, and it is no longer in flight. A record without it
	// whose process lives is a call that has not returned (InFlight).
	Ended bool `json:"ended,omitempty"`
	// Sent tells a call whose first write went out: it is marked right
	// before that write is sent (MarkSent), so a process killed after it
	// leaves the mark.
	Sent    bool      `json:"sent,omitempty"`
	Counts  bool      `json:"counts,omitempty"`
	Machine string    `json:"machine,omitempty"`
	CutSend time.Time `json:"cut_send,omitempty"`
	// Canon is the canonical form of the arguments Print is made of; not
	// saved. The caller sets it before Open and keeps it for Settle.
	Canon []byte `json:"-"`
	// Dir is the directory the intent was read from (Pending); not saved.
	Dir string `json:"-"`
}

// thisMachine is the mark of the machine evo runs on: a hash of its host
// name; "" when the system does not tell it — then no record is taken
// for one of this machine.
func thisMachine() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("evo-machine\x00" + host))
	return hex.EncodeToString(sum[:8])
}

// printOf is the print of the arguments canon in the record id.
func printOf(id string, canon []byte) string {
	h := sha256.New()
	h.Write([]byte(id))
	h.Write([]byte{0})
	h.Write(canon)
	return hex.EncodeToString(h.Sum(nil))
}

// SameCall tells that in is a record of a call with the arguments canon:
// by its print. A record without a print (evo before the print) is told by
// the caller, by its name and target.
func (in Intent) SameCall(canon []byte) bool {
	return in.Print != "" && canon != nil && in.Print == printOf(in.ID, canon)
}

func (in Intent) InFlight() bool {
	switch {
	case in.Ended || in.Held:
		return false
	case in.PID == os.Getpid():
		return openedHere(in.ID)
	}
	return processAlive(in.PID)
}

// openHere are the ids of the records this process opened (Open) whose
// call has not ended them (End, Hold, Close).
var openHere = struct {
	sync.Mutex
	ids map[string]bool
}{ids: map[string]bool{}}

func holdOpen(id string, open bool) {
	openHere.Lock()
	defer openHere.Unlock()
	if open {
		openHere.ids[id] = true
	} else {
		delete(openHere.ids, id)
	}
}

func openedHere(id string) bool {
	openHere.Lock()
	defer openHere.Unlock()
	return openHere.ids[id]
}

// MarkSent marks the record in as a call whose write goes out (Sent). An
// error: the mark is not written, and the write must not go out — the
// record would say, after a kill, that nothing was sent.
func MarkSent(in Intent) error {
	in.Sent = true
	if err := write(in); err != nil {
		return notWritten(in.Dir, err)
	}
	return nil
}

// HoldCut keeps the record in of a cut call whose change goes on as the
// operation evo goes on with (Held), marked as the cut send it is
// (CutSend at at): the call that waits for it opens no record of its own,
// and the trace of the cut send outlives the wait. Its line: unknown.
func HoldCut(in Intent, at time.Time) {
	journal(in, history.Outcome{Result: history.Unknown})
	in.Held, in.CutSend = true, at
	_ = write(in)
}

// MarkCutSend marks the held record in with a send of its operation that
// was cut at at (CutSend).
func MarkCutSend(in Intent, at time.Time) {
	in.CutSend = at
	_ = write(in)
}

// End marks the record in as a call that returned with its outcome not
// known: the record stays, and a later call of the same may settle it.
// Its line is unknown, with what out says of the answer (its code).
func End(in Intent, out history.Outcome) {
	defer holdOpen(in.ID, false)
	out.Result = history.Unknown
	journal(in, out)
	in.Ended = true
	_ = write(in)
}

// Dir is the directory of the intents of root's data directory: the calls
// of the tools, which the folder's state follows.
func Dir(root string) string { return filepath.Join(config.DataDir(root), "intents") }

func ConfigDir() string {
	if d := config.Dir(); d != "" {
		return filepath.Join(d, "intents")
	}
	return ""
}

// Dirs are the directories of intents a run in root reads: the folder's
// and the configuration's.
func Dirs(root string) []string {
	out := []string{Dir(root)}
	if c := ConfigDir(); c != "" && c != out[0] {
		out = append(out, c)
	}
	return out
}

// Begin writes in before the call goes out and returns the end of it:
// done writes the line of out and removes the file — or keeps it (End),
// when keep: the outcome is not known and the next run names it. An
// error means nothing was written: the caller refuses the call before it
// goes out.
func Begin(dir string, in Intent) (done func(out history.Outcome, keep bool), err error) {
	rec, err := Open(dir, in)
	if err != nil {
		return nil, err
	}
	return func(out history.Outcome, keep bool) {
		if keep {
			End(rec, out)
			return
		}
		Close(rec, out)
	}, nil
}

// Open writes in as Begin does and returns the record written, with its
// id and directory: Close removes it, Hold keeps it as an operation that
// goes on.
func Open(dir string, in Intent) (Intent, error) {
	if err := config.MkdirPrivate(dir); err != nil {
		return Intent{}, notWritten(dir, err)
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return Intent{}, notWritten(dir, err)
	}
	in.ID, in.Dir = hex.EncodeToString(b[:]), dir
	if in.Machine == "" {
		in.Machine = thisMachine()
	}
	if in.Canon != nil {
		in.Print = printOf(in.ID, in.Canon)
	}
	sort.Strings(in.Args)
	sort.Strings(in.Parties)
	if err := write(in); err != nil {
		return Intent{}, notWritten(dir, err)
	}
	if in.PID == os.Getpid() {
		holdOpen(in.ID, true)
	}
	return in, nil
}

// write writes the record in whole (temp file and rename).
func write(in Intent) error {
	path := recordPath(in.Dir, in.ID)
	if path == "" {
		return os.ErrInvalid
	}
	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(path, data, 0o600)
}

// Hold keeps the record in as an operation that goes on (Held), as the
// caller names it now: by the resource its next step names — a call that
// named a VM by name goes on by its id. A record that cannot be rewritten
// stays as it was written — a cut call, which is what the next run then
// says of it. Its line is unknown — the operation has not ended — with
// what out says of the answer: the resource, the operation.
func Hold(in Intent, out history.Outcome) {
	defer holdOpen(in.ID, false)
	out.Result = history.Unknown
	journal(in, out)
	in.Held = true
	_ = write(in)
}

func notWritten(dir string, err error) error {
	return clierrors.Wrap(clierrors.CodeConfigInvalid,
		i18n.Tf("could not write what evo is about to do to %s (%v); nothing was sent to the cloud",
			"не удалось записать, что evo собирается сделать, в %s (%v); в облако ничего не отправлено", dir, err),
		i18n.Tf("Make %s writable (evo keeps there what a cut call may have left in the cloud), then repeat the call.",
			"Сделайте каталог %s доступным для записи (evo хранит там, что мог оставить в облаке прерванный вызов) и повторите вызов.", filepath.Dir(dir)), err)
}

// idForm is the form of the id begin gives a record: its file's name.
var idForm = regexp.MustCompile(`^[0-9a-f]{16}$`)

// validID tells an id of evo's own form.
func validID(id string) bool { return idForm.MatchString(id) }

func recordPath(dir, id string) string {
	if !validID(id) {
		return ""
	}
	return filepath.Join(dir, id+".json")
}

func Pending(dir string) []Intent {
	all, _ := scan(dir)
	var out []Intent
	for _, in := range all {
		if in.Counts && !in.Sent && !in.Ended && !in.Held && in.Machine != "" && in.Machine == thisMachine() && !in.InFlight() {
			remove(dir, in.ID)
			continue
		}
		out = append(out, in)
	}
	return out
}

func Foreign(dir string) []string {
	_, foreign := scan(dir)
	return foreign
}

// scan reads dir once: the records of evo's form, and the paths of the
// other *.json files.
func scan(dir string) (out []Intent, foreign []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok {
			continue
		}
		var in Intent
		if e.Type().IsRegular() && validID(id) {
			if data, err := config.ReadFileNoFollow(recordPath(dir, id)); err == nil &&
				json.Unmarshal(data, &in) == nil && in.ID == id {
				in.Dir = dir
				out = append(out, in)
				continue
			}
		}
		foreign = append(foreign, filepath.Join(dir, e.Name()))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, foreign
}

func Settle(dir string, done Intent, out history.Outcome) (kept []Intent) {
	if done.Name == "" && done.Target == "" {
		return nil
	}
	for _, in := range Pending(dir) {
		if in.ID == done.ID || in.Tool != done.Tool || in.ProjectID != done.ProjectID {
			continue
		}
		if in.Print != "" {
			if !in.SameCall(done.Canon) || in.Target != "" && done.Target != "" && in.Target != done.Target ||
				in.Parties != nil && done.Parties != nil && !slices.Equal(in.Parties, done.Parties) {
				continue
			}
		} else if in.Name != done.Name || in.Target != done.Target {
			continue
		}
		if in.Key.Other(done.Key) {
			kept = append(kept, in)
			continue
		}
		if in.StartedAt.After(done.StartedAt) || in.Held || in.InFlight() || (in.Print != "" && in.Key == nil) {
			continue
		}
		journal(in, out)
		remove(dir, in.ID)
	}
	return kept
}

// Close removes the intent in: what the call did is now known, and out
// says it — the line of the journal is written first.
func Close(in Intent, out history.Outcome) {
	defer holdOpen(in.ID, false)
	journal(in, out)
	remove(in.Dir, in.ID)
}

func journal(in Intent, out history.Outcome) {
	if !validID(in.ID) || in.Dir == "" {
		return
	}
	surface := in.Surface
	if surface == "" {
		surface = "mcp"
	}
	l := history.Line{Time: clock.Now().UTC().Format(time.RFC3339), Tool: in.Tool, Surface: surface,
		ProjectID: in.ProjectID, Zone: in.Zone, Name: in.Name, Target: in.Target,
		Kind: out.Kind, Parent: out.Parent, ResourceID: out.ResourceID, Outcome: out.Result, Reason: out.Reason, OperationID: out.OperationID,
		IntentID: in.ID, JobID: out.JobID, StateDir: stateDirOf(in.Dir), Key: in.Key}
	if err := appendLine(history.DirOf(in.Dir), l); err != nil {
		config.Warn(i18n.Tf("the journal of changes in %s got no line of %s (%v); the call is as it answered",
			"в журнал изменений %s не записана строка %s (%v); вызов — как он ответил", history.DirOf(in.Dir), in.Tool, err))
	}
}

// appendLine writes a line of the journal; tests see the order of a line
// and its record through it.
var appendLine = history.Append

// stateDirOf is the folder of state of the records of dir: the root of
// its data directory; "" for the configuration's (evo login).
func stateDirOf(dir string) string {
	data := filepath.Dir(dir)
	if c := config.Dir(); c != "" && filepath.Clean(c) == filepath.Clean(data) {
		return ""
	}
	return filepath.Dir(data)
}

// remove removes the record id of dir; an id not of evo's form removes
// nothing.
func remove(dir, id string) {
	if path := recordPath(dir, id); path != "" {
		// A concurrent read of the record by another call blocks the
		// removal on Windows: it is retried (config.YieldingToReaders).
		_ = config.YieldingToReaders(func() error { return os.Remove(path) })
	}
}
