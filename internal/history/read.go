// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package history

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloud-ru/evo-cmcp/internal/config"
)

// fileForm is the name of a file of a day: anything else in the directory
// is not read.
var fileForm = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.jsonl$`)

// intentForm is the form of the id of a record of an intent.
var intentForm = regexp.MustCompile(`^[0-9a-f]{16}$`)

const maxText = 2048

// maxLine is the longest line read: every field at maxText still fits.
// A longer line is not of evo's; it is skipped by itself, and the lines
// after it are read.
const maxLine = 64 << 10

type Journal struct {
	Lines   []Line
	Skipped int
	// taken are the lines taken, as they are: one met twice is one.
	// skipped are the lines not taken, counted once each. A line not taken
	// where it stands — another day's in the file of a day — shadows no
	// copy of it that may be taken where that stands.
	taken, skipped map[string]bool
}

// Check is what a reader knows of a line beyond its form: the tool is of
// the registry, the reason is a code of evo. nil checks the form only.
type Check func(l Line) bool

// ReadAll reads every file of a day in dir, oldest first, then the live
// lines of the files pruned (LiveName) and the lines that stood in a file
// not their day's (StrayName), which it skips. A file that is not a regular one of
// evo's form — a link, a directory, another name — is not read. A line
// read twice — a pruning killed between keeping a live line and removing
// its file — is one line.
func ReadAll(dir string, check Check) (Journal, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return Journal{}, nil
		}
		return Journal{}, err
	}
	var names []string
	for _, e := range entries {
		if e.Type().IsRegular() && fileForm.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	names = readOrder(names)
	j := Journal{taken: map[string]bool{}, skipped: map[string]bool{}}
	retired := RetiredThrough(dir)
	for _, name := range names {
		data, err := config.ReadFileNoFollow(filepath.Join(dir, name))
		if err != nil {
			if !os.IsNotExist(err) {
				j.Skipped++
			}
			continue
		}
		j.read(data, fileCheck(name, retired, check))
	}
	return j, nil
}

func readOrder(days []string) []string {
	return append(append([]string(nil), days...), LiveName, StrayName)
}

func ofFile(name, retired string, l Line) bool {
	switch name {
	case LiveName:
		return retired != "" && l.Time[:len(time.DateOnly)] <= retired
	case StrayName:
		return false
	}
	return strings.HasPrefix(l.Time, strings.TrimSuffix(name, ".jsonl")+"T")
}

// fileCheck is check for the lines of the file name: a line not of its file
// (ofFile) is not taken, as a line check refuses. The reader and the pruning
// take the lines of a file by it alike.
func fileCheck(name, retired string, check Check) Check {
	return func(l Line) bool {
		return ofFile(name, retired, l) && (check == nil || check(l))
	}
}

// read adds the lines of the file data.
func (j *Journal) read(data []byte, check Check) {
	for _, raw := range lines(data) {
		if j.taken[string(raw)] {
			continue
		}
		l, ok := parse(raw)
		if !ok || (check != nil && !check(l)) {
			if !j.skipped[string(raw)] {
				j.skipped[string(raw)] = true
				j.Skipped++
			}
			continue
		}
		j.taken[string(raw)] = true
		j.Lines = append(j.Lines, l)
	}
}

func parse(raw []byte) (Line, bool) {
	var l Line
	if len(raw) > maxLine || !utf8.Valid(raw) || loneSurrogate(raw) || json.Unmarshal(raw, &l) != nil || !wellFormed(l) {
		return Line{}, false
	}
	return l, true
}

func loneSurrogate(raw []byte) bool {
	surrogate := func(i int) (rune, bool) {
		if i+6 > len(raw) || raw[i] != '\\' || raw[i+1] != 'u' {
			return 0, false
		}
		var r rune
		for _, c := range raw[i+2 : i+6] {
			switch {
			case c >= '0' && c <= '9':
				r = r<<4 | rune(c-'0')
			case c >= 'a' && c <= 'f':
				r = r<<4 | rune(c-'a'+10)
			case c >= 'A' && c <= 'F':
				r = r<<4 | rune(c-'A'+10)
			default:
				return 0, false
			}
		}
		return r, true
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		r, ok := surrogate(i)
		switch {
		case !ok:
			i++ // \\, \", \n…: the next byte is the escape's own
		case r >= 0xD800 && r <= 0xDBFF:
			if low, ok := surrogate(i + 6); !ok || low < 0xDC00 || low > 0xDFFF {
				return true
			}
			i += 11
		case r >= 0xDC00 && r <= 0xDFFF:
			return true
		default:
			i += 5
		}
	}
	return false
}

func lines(data []byte) [][]byte {
	var out [][]byte
	for _, raw := range bytes.Split(data, []byte{'\n'}) {
		raw = bytes.TrimRight(raw, "\r")
		if len(bytes.TrimSpace(raw)) != 0 {
			out = append(out, raw)
		}
	}
	return out
}

// wellFormed tells a line of evo's form: this format, an outcome of the
// list, a time in UTC as evo writes it — the lines are ordered by it —
// the id of a record, texts of a bounded length. A text may hold a line
// break: it is a value in JSON, escaped, on the line of its record; the
// answer marks it untrusted and the table of the CLI cleans it.
func wellFormed(l Line) bool {
	if l.Format != Format || !Known(l.Outcome) || !intentForm.MatchString(l.IntentID) || l.Tool == "" {
		return false
	}
	if at, err := time.Parse(time.RFC3339, l.Time); err != nil || at.UTC().Format(time.RFC3339) != l.Time {
		return false
	}
	texts := []string{l.Tool, l.Surface, l.ProjectID, l.Zone, l.Name, l.Target, l.Kind, l.Parent, l.ResourceID, l.Reason, l.OperationID, l.JobID, l.StateDir}
	if l.Key != nil {
		texts = append(texts, l.Key.ServiceAccountID, l.Key.KeyID)
	}
	for _, s := range texts {
		if len(s) > maxText {
			return false
		}
	}
	return true
}

// Since are the lines of j written at since or after.
func (j Journal) Since(since time.Time) []Line {
	var out []Line
	for _, l := range j.Lines {
		if at, _ := time.Parse(time.RFC3339, l.Time); !at.Before(since) {
			out = append(out, l)
		}
	}
	return out
}
