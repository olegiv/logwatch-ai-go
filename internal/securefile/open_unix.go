//go:build darwin || linux

// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

// Package securefile provides no-follow file access for operator-configured
// inputs that may be read by a root cron process.
package securefile

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// EnsurePrivateDirectory creates a missing directory hierarchy without
// following symlinks and validates that the final directory is already owned
// by the current process with mode 0700. Existing directories are never
// chowned or chmodded: a typo in a privileged DATABASE_PATH must not mutate a
// shared system directory such as / or /var.
func EnsurePrivateDirectory(path string) (string, error) {
	return trustedDirectory(path, true, 0o700, true, "database directory")
}

// ValidatePrivateDirectory validates the existing portion of a private
// directory path without creating anything. A missing suffix is acceptable
// because EnsurePrivateDirectory can later create it below the validated
// ancestor using no-follow traversal.
func ValidatePrivateDirectory(path string) (string, error) {
	return trustedDirectory(path, false, 0o700, true, "database directory")
}

// EnsureTrustedDirectory creates a missing directory hierarchy without
// following symlinks. Every existing component must be owned by root or the
// current user and must not be writable by another account (except sticky
// system directories such as /tmp). The final directory must be owned by the
// current user and not group/world-writable.
func EnsureTrustedDirectory(path string, createMode os.FileMode) (string, error) {
	return trustedDirectory(path, true, createMode, false, "directory")
}

// ValidateTrustedDirectory performs the same hierarchy checks without
// creating a missing suffix. It is intended for read-only runtime preflight.
func ValidateTrustedDirectory(path string) (string, error) {
	return trustedDirectory(path, false, 0, false, "directory")
}

func trustedDirectory(
	path string,
	createMissing bool,
	createMode os.FileMode,
	requireExactMode bool,
	label string,
) (string, error) {
	cleaned, err := cleanAbsolutePath(path)
	if err != nil {
		return "", err
	}
	if cleaned == string(filepath.Separator) {
		return "", fmt.Errorf("%s must not be the filesystem root", label)
	}
	if createMissing && (createMode.Perm() == 0 || createMode.Perm()&0o022 != 0) {
		return "", fmt.Errorf("new %s mode must not be group/world-writable", label)
	}
	cleaned, err = resolveTrustedRootAlias(cleaned)
	if err != nil {
		return "", err
	}

	currentFD, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return "", fmt.Errorf("open filesystem root: %w", err)
	}
	defer func() { _ = unix.Close(currentFD) }()

	components := strings.Split(strings.TrimPrefix(cleaned, string(filepath.Separator)), string(filepath.Separator))
	for i, component := range components {
		nextFD, missing, openErr := openDirectoryComponent(
			currentFD, component, cleaned, label, createMissing, createMode,
		)
		if missing {
			return cleaned, nil
		}
		if openErr != nil {
			return "", fmt.Errorf("open directory component %q: %w", component, openErr)
		}

		var stat unix.Stat_t
		if err := unix.Fstat(nextFD, &stat); err != nil {
			_ = unix.Close(nextFD)
			return "", fmt.Errorf("inspect directory component %q: %w", component, err)
		}
		mode := os.FileMode(stat.Mode).Perm()
		isFinal := i == len(components)-1
		if isFinal {
			if err := validateFinalDirectory(stat.Uid, mode, cleaned, label, createMode, requireExactMode); err != nil {
				_ = unix.Close(nextFD)
				return "", err
			}
		} else {
			if err := validateDirectoryAncestor(stat.Uid, uint32(stat.Mode), os.Geteuid()); err != nil {
				_ = unix.Close(nextFD)
				return "", fmt.Errorf("untrusted directory ancestor for %s: %w", cleaned, err)
			}
		}

		_ = unix.Close(currentFD)
		currentFD = nextFD
	}
	return cleaned, nil
}

func openDirectoryComponent(
	parentFD int,
	component, cleaned, label string,
	createMissing bool,
	createMode os.FileMode,
) (fd int, missing bool, err error) {
	flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_DIRECTORY
	fd, err = unix.Openat(parentFD, component, flags, 0)
	if err == nil || !errors.Is(err, unix.ENOENT) {
		return fd, false, err
	}
	if !createMissing {
		return -1, true, nil
	}
	if err := unix.Mkdirat(parentFD, component, uint32(createMode.Perm())); err != nil {
		return -1, false, fmt.Errorf("create %s %s: %w", label, cleaned, err)
	}
	fd, err = unix.Openat(parentFD, component, flags, 0)
	return fd, false, err
}

func validateFinalDirectory(
	uid uint32,
	mode os.FileMode,
	cleaned, label string,
	createMode os.FileMode,
	requireExactMode bool,
) error {
	modeInvalid := mode&0o022 != 0
	if requireExactMode {
		modeInvalid = mode != createMode.Perm()
	}
	if int(uid) == os.Geteuid() && !modeInvalid {
		return nil
	}
	if requireExactMode {
		return fmt.Errorf(
			"existing %s must be owned by uid %d with mode %04o: %s",
			label, os.Geteuid(), createMode.Perm(), cleaned,
		)
	}
	return fmt.Errorf(
		"existing %s must be owned by uid %d and not group/world-writable: %s",
		label, os.Geteuid(), cleaned,
	)
}

func cleanAbsolutePath(path string) (string, error) {
	cleaned, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	return cleaned, nil
}

// resolveTrustedRootAlias permits immutable, root-owned aliases such as
// macOS /var -> private/var without allowing symlinks deeper in a configured
// path. A non-root account cannot replace an entry in the root directory.
func resolveTrustedRootAlias(path string) (string, error) {
	components := strings.Split(strings.TrimPrefix(path, string(filepath.Separator)), string(filepath.Separator))
	if len(components) == 0 || components[0] == "" {
		return path, nil
	}
	first := filepath.Join(string(filepath.Separator), components[0])
	info, err := os.Lstat(first)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return path, nil
	}
	var stat unix.Stat_t
	if err := unix.Lstat(first, &stat); err != nil {
		return "", fmt.Errorf("inspect root path alias %s: %w", first, err)
	}
	if stat.Uid != 0 {
		return "", fmt.Errorf("root path alias must be owned by uid 0: %s", first)
	}
	target, err := os.Readlink(first)
	if err != nil {
		return "", fmt.Errorf("read root path alias %s: %w", first, err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(string(filepath.Separator), target)
	}
	return filepath.Join(append([]string{filepath.Clean(target)}, components[1:]...)...), nil
}

func validateDirectoryAncestor(uid uint32, rawMode uint32, effectiveUID int) error {
	if int(uid) != effectiveUID && uid != 0 {
		return fmt.Errorf("owned by untrusted uid %d", uid)
	}
	mode := os.FileMode(rawMode).Perm()
	if mode&0o022 != 0 && rawMode&unix.S_ISVTX == 0 {
		return fmt.Errorf("writable by another account")
	}
	return nil
}

// PreparePrivateRegular creates path with mode 0600 or validates an existing
// regular file. Existing files are never chowned or chmodded.
func PreparePrivateRegular(path string) error {
	fd, err := unix.Open(path, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0o600)
	if errors.Is(err, unix.EEXIST) {
		fd, err = unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	}
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect opened file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("path is not a regular file: %s", path)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("inspect database file ownership: %w", err)
	}
	if int(stat.Uid) != os.Geteuid() || info.Mode().Perm() != 0o600 {
		return fmt.Errorf(
			"existing database file must be owned by uid %d with mode 0600: %s",
			os.Geteuid(), path,
		)
	}
	return nil
}

// ValidatePrivateRegular validates an existing private regular file without
// creating or changing it. A missing file is acceptable because
// PreparePrivateRegular can later create it atomically with mode 0600.
func ValidatePrivateRegular(path string) error {
	file, err := OpenNoFollow(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("inspect opened file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("path is not a regular file: %s", path)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &stat); err != nil {
		return fmt.Errorf("inspect database file ownership: %w", err)
	}
	if int(stat.Uid) != os.Geteuid() || info.Mode().Perm() != 0o600 {
		return fmt.Errorf(
			"existing database file must be owned by uid %d with mode 0600: %s",
			os.Geteuid(), path,
		)
	}
	return nil
}

// OpenNoFollow opens path read-only without following symbolic links in any
// component. The returned handle owns the file descriptor.
func OpenNoFollow(path string) (*os.File, error) {
	cleaned, err := cleanAbsolutePath(path)
	if err != nil {
		return nil, err
	}
	cleaned, err = resolveTrustedRootAlias(cleaned)
	if err != nil {
		return nil, err
	}

	currentFD, err := unix.Open(string(filepath.Separator), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY, 0)
	if err != nil {
		return nil, fmt.Errorf("open filesystem root: %w", err)
	}
	components := strings.Split(strings.TrimPrefix(cleaned, string(filepath.Separator)), string(filepath.Separator))
	for i, component := range components {
		isFinal := i == len(components)-1
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		if !isFinal {
			flags |= unix.O_DIRECTORY
		}
		nextFD, openErr := unix.Openat(currentFD, component, flags, 0)
		_ = unix.Close(currentFD)
		if openErr != nil {
			return nil, &os.PathError{Op: "open", Path: cleaned, Err: openErr}
		}
		if isFinal {
			return os.NewFile(uintptr(nextFD), cleaned), nil
		}
		currentFD = nextFD
	}

	_ = unix.Close(currentFD)
	return nil, fmt.Errorf("path must name a file: %s", cleaned)
}

// ReadLimitedRegular reads a regular file without following symbolic links and
// enforces a hard byte limit against the opened inode.
func ReadLimitedRegular(path string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("invalid file size limit: %d", maxBytes)
	}
	file, err := OpenNoFollow(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path is not a regular file: %s", path)
	}
	if info.Size() > maxBytes {
		return nil, fmt.Errorf("file too large: exceeds %d bytes", maxBytes)
	}
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file grew beyond %d bytes while being read", maxBytes)
	}
	return data, nil
}
