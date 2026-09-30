// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package logging

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateLogDestinationRejectsSymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "analyzer.log")); err != nil {
		t.Fatalf("create symlink: %v", err)
	}
	if err := ValidateLogDestination(dir, "analyzer.log"); err == nil {
		t.Fatal("ValidateLogDestination() should reject symlink")
	}
}

func TestValidateLogDestinationRejectsWritableAncestor(t *testing.T) {
	t.Parallel()

	shared := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(shared, 0o777); err != nil {
		t.Fatalf("create shared parent: %v", err)
	}
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatalf("make shared parent writable: %v", err)
	}
	logDir := filepath.Join(shared, "logs")
	if err := os.Mkdir(logDir, 0o750); err != nil {
		t.Fatalf("create log directory: %v", err)
	}

	if err := ValidateLogDestination(logDir, "analyzer.log"); err == nil {
		t.Fatal("ValidateLogDestination() accepted an attacker-writable ancestor")
	}
}

func TestValidateLogDestinationReadOnlyDoesNotCreateMissingPath(t *testing.T) {
	t.Parallel()

	logDir := filepath.Join(t.TempDir(), "missing", "logs")
	if err := ValidateLogDestinationReadOnly(logDir, "analyzer.log"); err != nil {
		t.Fatalf("ValidateLogDestinationReadOnly() error = %v", err)
	}
	if _, err := os.Lstat(logDir); !os.IsNotExist(err) {
		t.Fatalf("read-only validation created %s: %v", logDir, err)
	}
}
