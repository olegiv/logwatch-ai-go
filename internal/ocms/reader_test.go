// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ocms

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

var _ analyzer.LogReader = (*Reader)(nil)

func TestReader_Read_Success(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "ocms.log")
	content := strings.Repeat("2026-04-26T02:15:00Z INFO request processed successfully\n", 4)
	if err := os.WriteFile(testFile, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	reader := NewReader(10, false, 1000)
	got, err := reader.Read(testFile)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got != content {
		t.Errorf("Read() content mismatch")
	}
}

func TestReader_Read_NotFound(t *testing.T) {
	t.Parallel()

	reader := NewReader(10, false, 1000)
	_, err := reader.Read("/tmp/does-not-exist-ocms.log")
	if err == nil || !strings.Contains(err.Error(), "ocms log file not found") {
		t.Fatalf("expected not found error, got: %v", err)
	}
}

func TestReader_Read_TooOld(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "ocms.log")
	content := strings.Repeat("2026-04-26T02:15:00Z WARN high latency\n", 4)
	if err := os.WriteFile(testFile, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(testFile, old, old); err != nil {
		t.Fatalf("failed to set mtime: %v", err)
	}

	reader := NewReader(10, false, 1000)
	_, err := reader.Read(testFile)
	if err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatalf("expected too old error, got: %v", err)
	}
}

func TestReader_Validate(t *testing.T) {
	t.Parallel()

	reader := NewReader(10, false, 1000)
	if err := reader.Validate(""); err == nil {
		t.Fatal("expected validation error for empty content")
	}
	if err := reader.Validate(strings.Repeat("x", 99)); err == nil {
		t.Fatal("expected validation error for small content")
	}
	if err := reader.Validate(strings.Repeat("x", 120)); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
