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
	if err := os.WriteFile(testFile, []byte(content), 0o600); err != nil {
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

func TestReader_Read_ShortNonEmptyLog(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "ocms.log")
	content := "WARN short OCMS event\n"
	if err := os.WriteFile(testFile, []byte(content), 0o600); err != nil {
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

func TestReader_ReadFiles_CombinedContent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	mainLog := filepath.Join(tmpDir, "ocms.log")
	errorLog := filepath.Join(tmpDir, "error.log")
	mainContent := "2026-04-26T02:15:00Z INFO main log event\n"
	errorContent := "2026-04-26T02:15:01Z ERROR error log event\n"
	if err := os.WriteFile(mainLog, []byte(mainContent), 0o600); err != nil {
		t.Fatalf("failed to write main log: %v", err)
	}
	if err := os.WriteFile(errorLog, []byte(errorContent), 0o600); err != nil {
		t.Fatalf("failed to write error log: %v", err)
	}

	reader := NewReader(10, false, 1000)
	got, err := reader.ReadFiles([]LogFile{
		{Kind: "main", Path: mainLog},
		{Kind: "error", Path: errorLog},
	})
	if err != nil {
		t.Fatalf("ReadFiles() error = %v", err)
	}
	for _, want := range []string{
		"### OCMS MAIN LOG",
		"### OCMS ERROR LOG",
		"Path: " + mainLog,
		"Path: " + errorLog,
		mainContent,
		errorContent,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("combined content missing %q:\n%s", want, got)
		}
	}
}

func TestReader_ReadFiles_MissingSecondFileFails(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	mainLog := filepath.Join(tmpDir, "ocms.log")
	errorLog := filepath.Join(tmpDir, "error.log")
	if err := os.WriteFile(mainLog, []byte("2026-04-26T02:15:00Z INFO main log event\n"), 0o600); err != nil {
		t.Fatalf("failed to write main log: %v", err)
	}

	reader := NewReader(10, false, 1000)
	_, err := reader.ReadFiles([]LogFile{
		{Kind: "main", Path: mainLog},
		{Kind: "error", Path: errorLog},
	})
	if err == nil {
		t.Fatal("ReadFiles() expected error for missing second file")
	}
	if !strings.Contains(err.Error(), "failed to read OCMS error log") {
		t.Fatalf("error = %v", err)
	}
}

func TestReader_Read_TooOld(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "ocms.log")
	content := strings.Repeat("2026-04-26T02:15:00Z WARN high latency\n", 4)
	if err := os.WriteFile(testFile, []byte(content), 0o600); err != nil {
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
	if err := reader.Validate("x"); err != nil {
		t.Fatalf("unexpected validation error for short content: %v", err)
	}
	if err := reader.Validate(strings.Repeat("x", 120)); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}
