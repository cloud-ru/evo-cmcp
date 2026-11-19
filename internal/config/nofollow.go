// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"io"
	"os"

	"github.com/cloud-ru/evo-cmcp/internal/i18n"
)

func OpenNoFollow(path string, flag int, perm os.FileMode) (*os.File, error) {
	before, lerr := os.Lstat(path)
	if lerr == nil && !before.Mode().IsRegular() {
		return nil, notRegular(path)
	}
	f, err := os.OpenFile(path, flag|oNoFollow, perm)
	if err != nil {
		if after, aerr := os.Lstat(path); aerr == nil && !after.Mode().IsRegular() {
			return nil, notRegular(path)
		}
		return nil, err
	}
	// The file opened is the one at path now, as itself: the one Lstat saw,
	// or — created by this open — no link put in place between the two
	// (where O_NOFOLLOW is no flag).
	got, err := f.Stat()
	if err == nil && lerr != nil {
		before, lerr = os.Lstat(path)
	}
	if err != nil || lerr != nil || !before.Mode().IsRegular() || !os.SameFile(before, got) {
		f.Close()
		return nil, notRegular(path)
	}
	return f, nil
}

func ReadFileNoFollow(path string) ([]byte, error) {
	f, err := OpenNoFollow(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func notRegular(path string) error {
	return fmt.Errorf(i18n.T("%s is not a regular file (a symlink?): evo does not write or read through it; delete it",
		"%s — не обычный файл (символьная ссылка?): evo не пишет и не читает через него; удалите его"), path)
}
