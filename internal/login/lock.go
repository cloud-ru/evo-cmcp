// SPDX-FileCopyrightText: 2026 ООО «Облачные технологии» (Cloud.ru)
// SPDX-License-Identifier: Apache-2.0

package login

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cloud-ru/evo-cmcp/internal/config"
	clierrors "github.com/cloud-ru/evo-cmcp/internal/errors"
)

type loginLock struct{ f *os.File }

func lockLogin(path string) (*loginLock, error) {
	if err := config.MkdirPrivate(filepath.Dir(path)); err != nil {
		return nil, at(stepLock, err)
	}
	f, err := config.OpenNoFollow(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, at(stepLock, err)
	}
	if err := config.LockFile(f); err != nil {
		held, _ := io.ReadAll(io.NewSectionReader(f, 0, 64))
		_ = f.Close()
		if errors.Is(err, config.ErrLocked) {
			return nil, at(stepLock, clierrors.New(clierrors.CodeStateLocked, eLoginRunning(filepath.Dir(path), portOf(string(held))), eLoginRunningHint()))
		}
		return nil, at(stepLock, err)
	}
	return &loginLock{f: f}, nil
}

// note writes the holder's pid and port into the file.
func (l *loginLock) note(port int) {
	_ = l.f.Truncate(0)
	_, _ = l.f.WriteAt([]byte(fmt.Sprintf("%d %d\n", os.Getpid(), port)), 0)
}

func (l *loginLock) release() {
	_ = l.f.Truncate(0)
	_ = config.UnlockFile(l.f)
	_ = l.f.Close()
}

// portOf is the port of «pid port», 0 when unknown.
func portOf(held string) int {
	f := strings.Fields(held)
	if len(f) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(f[1])
	return n
}
