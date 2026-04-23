// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package ocms

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sampleSlog is a short block of valid ocms-go slog text-handler output
// used across tests. Each line matches slogShapeRegex.
const sampleSlog = `time=2026-04-23T10:15:42.123Z level=info msg="Server started" addr=:8080
time=2026-04-23T10:15:43.456Z level=info msg="Page created" id=42 title="About"
time=2026-04-23T10:16:01.789Z level=warn msg="Cache miss rate high" duration_ms=234
time=2026-04-23T10:16:15.111Z level=error msg="Database query failed" error="connection timeout"
time=2026-04-23T10:16:20.222Z level=info msg="Login failed" category=auth ip_address=192.168.1.5
`

func TestNewReader(t *testing.T) {
	r := NewReader(10, true, 150000)
	if r == nil {
		t.Fatal("expected reader")
	}
	if r.maxSizeMB != 10 || !r.enablePreprocessing || r.maxTokens != 150000 {
		t.Errorf("reader fields not set correctly: %+v", r)
	}
	if r.preprocessor == nil {
		t.Error("preprocessor should be initialized")
	}
}

func TestRead_FileNotFound(t *testing.T) {
	r := NewReader(10, false, 150000)
	_, err := r.Read("/nonexistent/ocms.log")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected 'not found' error, got: %v", err)
	}
}

func TestRead_ValidFile(t *testing.T) {
	tmp := writeTempFile(t, sampleSlog)
	r := NewReader(10, false, 150000)
	got, err := r.Read(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != sampleSlog {
		t.Error("content mismatch")
	}
}

func TestRead_FileTooBig(t *testing.T) {
	tmp := writeTempFile(t, strings.Repeat("X", 2*1024*1024))
	r := NewReader(1, false, 150000)
	_, err := r.Read(tmp)
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("expected size-limit error, got: %v", err)
	}
}

func TestRead_FileTooOld(t *testing.T) {
	tmp := writeTempFile(t, sampleSlog)
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(tmp, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	r := NewReader(10, false, 150000)
	_, err := r.Read(tmp)
	if err == nil || !strings.Contains(err.Error(), "too old") {
		t.Fatalf("expected 'too old' error, got: %v", err)
	}
}

func TestRead_Empty(t *testing.T) {
	tmp := writeTempFile(t, "")
	r := NewReader(10, false, 150000)
	_, err := r.Read(tmp)
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected 'empty' error, got: %v", err)
	}
}

func TestRead_TooSmall(t *testing.T) {
	tmp := writeTempFile(t, "short")
	r := NewReader(10, false, 150000)
	_, err := r.Read(tmp)
	if err == nil || !strings.Contains(err.Error(), "too small") {
		t.Fatalf("expected 'too small' error, got: %v", err)
	}
}

func TestRead_NotSlogShape(t *testing.T) {
	tmp := writeTempFile(t, strings.Repeat("just some regular text that is long enough but lacks slog shape\n", 5))
	r := NewReader(10, false, 150000)
	_, err := r.Read(tmp)
	if err == nil || !strings.Contains(err.Error(), "slog") {
		t.Fatalf("expected slog shape error, got: %v", err)
	}
}

func TestRead_WithPreprocessing(t *testing.T) {
	content := strings.Repeat(sampleSlog, 2000)
	tmp := writeTempFile(t, content)
	r := NewReader(10, true, 500) // tiny budget to force preprocessing
	got, err := r.Read(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == "" {
		t.Error("expected non-empty result after preprocessing")
	}
	if len(got) >= len(content) {
		t.Error("expected preprocessing to shrink content")
	}
}

func TestValidate(t *testing.T) {
	r := NewReader(10, false, 150000)
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{"valid", sampleSlog, false},
		{"empty", "", true},
		{"too small", "tiny", true},
		{"no slog shape", strings.Repeat("plain text line without slog shape\n", 5), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := r.Validate(tt.content)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestGetSourceInfo(t *testing.T) {
	tmp := writeTempFile(t, sampleSlog)
	r := NewReader(10, false, 150000)
	info, err := r.GetSourceInfo(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, key := range []string{"size_bytes", "size_mb", "modified", "age_hours"} {
		if _, ok := info[key]; !ok {
			t.Errorf("missing key %q", key)
		}
	}
	if sz, ok := info["size_bytes"].(int64); !ok || sz != int64(len(sampleSlog)) {
		t.Errorf("size_bytes = %v, want %d", info["size_bytes"], len(sampleSlog))
	}
}

func TestGetSourceInfo_NotFound(t *testing.T) {
	r := NewReader(10, false, 150000)
	if _, err := r.GetSourceInfo("/nonexistent/ocms.log"); err == nil {
		t.Error("expected error for missing file")
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ocms.log")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}
