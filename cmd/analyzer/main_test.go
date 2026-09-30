// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/olegiv/logwatch-ai-go/internal/config"
)

func TestValidateRuntimeRejectsInvalidLogDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "target")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatalf("create target: %v", err)
	}
	link := filepath.Join(tmpDir, "logs")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create log-directory symlink: %v", err)
	}

	err := validateRuntime(&config.Config{LogDir: link})
	if err == nil || !strings.Contains(err.Error(), "logging") {
		t.Fatalf("validateRuntime() error = %v, want logging failure", err)
	}
}
