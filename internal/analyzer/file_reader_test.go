package analyzer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadSourceFileWithGuards_Success(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "sample.log")
	content := strings.Repeat("line with enough content\n", 8)
	if err := os.WriteFile(f, []byte(content), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	got, err := ReadSourceFileWithGuards(f, FileReadOptions{
		SourceLabel: "sample",
		MaxSizeMB:   10,
		MaxAge:      24 * time.Hour,
	}, func(c string) error {
		if len(c) < 100 {
			t.Fatalf("expected content length >= 100, got %d", len(c))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ReadSourceFileWithGuards() error = %v", err)
	}
	if got != content {
		t.Fatalf("content mismatch")
	}
}

func TestReadSourceFileWithGuards_NotFound(t *testing.T) {
	t.Parallel()

	_, err := ReadSourceFileWithGuards("/tmp/no-such-file.log", FileReadOptions{
		SourceLabel: "sample",
		MaxSizeMB:   10,
		MaxAge:      24 * time.Hour,
	}, func(string) error { return nil })

	if err == nil || !strings.Contains(err.Error(), "sample file not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadSourceFileWithGuards_TooOld(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	f := filepath.Join(tmpDir, "sample.log")
	content := strings.Repeat("line with enough content\n", 8)
	if err := os.WriteFile(f, []byte(content), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(f, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	_, err := ReadSourceFileWithGuards(f, FileReadOptions{
		SourceLabel: "sample",
		MaxSizeMB:   10,
		MaxAge:      24 * time.Hour,
	}, func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatalf("unexpected error: %v", err)
	}
}
