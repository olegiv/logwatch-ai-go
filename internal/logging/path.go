// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package logging

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/olegiv/logwatch-ai-go/internal/securefile"
)

// ValidateLogDestination prevents a privileged analyzer from opening a log
// path that an unprivileged account can replace with a symlink.
func ValidateLogDestination(dir, filename string) error {
	return validateLogDestination(dir, filename, true)
}

// ValidateLogDestinationReadOnly validates a logging destination without
// creating the directory or file. A missing suffix is accepted when its
// nearest existing ancestor is trusted, so deployments can preflight a clean
// installation without mutating it.
func ValidateLogDestinationReadOnly(dir, filename string) error {
	return validateLogDestination(dir, filename, false)
}

func validateLogDestination(dir, filename string, createMissing bool) error {
	if filename == "" || filepath.Base(filename) != filename {
		return fmt.Errorf("log filename must be a basename")
	}

	var canonicalDir string
	var err error
	if createMissing {
		canonicalDir, err = securefile.EnsureTrustedDirectory(dir, 0o750)
	} else {
		canonicalDir, err = securefile.ValidateTrustedDirectory(dir)
	}
	if err != nil {
		return fmt.Errorf("invalid log directory: %w", err)
	}

	path := filepath.Join(canonicalDir, filename)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect log file: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("log destination must be a regular file: %s", path)
	}
	if os.Geteuid() == 0 {
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 || info.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("privileged log file must be root-owned and not group/world-writable: %s", path)
		}
	}
	return nil
}
