// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package securefile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPreparePrivateRegularRejectsSymlinkWithoutChangingTarget(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	link := filepath.Join(dir, "database")
	if err := os.WriteFile(target, []byte("sentinel"), 0o644); err != nil {
		t.Fatalf("create target: %v", err)
	}
	if err := os.Chmod(target, 0o644); err != nil {
		t.Fatalf("set target mode: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	if err := PreparePrivateRegular(link); err == nil {
		t.Fatal("PreparePrivateRegular() accepted a symlink")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(data) != "sentinel" {
		t.Fatalf("target contents = %q, want sentinel", data)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("target mode = %04o, want 0644", got)
	}
}

func TestEnsurePrivateDirectoryRejectsSharedWritableParent(t *testing.T) {
	t.Parallel()

	parent := filepath.Join(t.TempDir(), "shared")
	if err := os.Mkdir(parent, 0o777); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if err := os.Chmod(parent, 0o777); err != nil {
		t.Fatalf("set parent mode: %v", err)
	}
	dir := filepath.Join(parent, "database")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("create database directory: %v", err)
	}

	if _, err := EnsurePrivateDirectory(dir); err == nil {
		t.Fatal("EnsurePrivateDirectory() accepted a shared-writable parent")
	}
}

func TestValidateDirectoryAncestorRejectsForeignOwner(t *testing.T) {
	t.Parallel()

	if err := validateDirectoryAncestor(2000, uint32(0o755), 1000); err == nil {
		t.Fatal("validateDirectoryAncestor() accepted a foreign-owned ancestor")
	}
	if err := validateDirectoryAncestor(0, uint32(0o755), 1000); err != nil {
		t.Fatalf("validateDirectoryAncestor() rejected a root-owned ancestor: %v", err)
	}
	if err := validateDirectoryAncestor(1000, uint32(0o755), 1000); err != nil {
		t.Fatalf("validateDirectoryAncestor() rejected a current-user ancestor: %v", err)
	}
}

func TestOpenNoFollowRejectsIntermediateSymlink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	protected := filepath.Join(dir, "protected")
	if err := os.Mkdir(protected, 0o700); err != nil {
		t.Fatalf("create protected directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(protected, "source.log"), []byte("secret"), 0o600); err != nil {
		t.Fatalf("create protected file: %v", err)
	}
	link := filepath.Join(dir, "logs")
	if err := os.Symlink(protected, link); err != nil {
		t.Fatalf("create intermediate symlink: %v", err)
	}

	if _, err := OpenNoFollow(filepath.Join(link, "source.log")); err == nil {
		t.Fatal("OpenNoFollow() accepted an intermediate symlink")
	}
}

func TestEnsurePrivateDirectoryDoesNotMutateExistingDirectory(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "database")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("create directory: %v", err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("set directory mode: %v", err)
	}

	if _, err := EnsurePrivateDirectory(dir); err == nil {
		t.Fatal("EnsurePrivateDirectory() accepted a non-private directory")
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat directory: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o755 {
		t.Fatalf("directory mode changed to %04o, want 0755", got)
	}
}

func TestPreparePrivateRegularDoesNotMutateExistingFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "database")
	if err := os.WriteFile(path, []byte("sentinel"), 0o644); err != nil {
		t.Fatalf("create file: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("set file mode: %v", err)
	}

	if err := PreparePrivateRegular(path); err == nil {
		t.Fatal("PreparePrivateRegular() accepted a non-private file")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("file mode changed to %04o, want 0644", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(data) != "sentinel" {
		t.Fatalf("file content changed to %q", data)
	}
}
