package analyzer

import (
	"testing"
)

func TestValidSourceTypes(t *testing.T) {
	types := ValidSourceTypes()
	if len(types) != 3 {
		t.Errorf("ValidSourceTypes() returned %d items, want 3", len(types))
	}

	expected := map[string]bool{
		"logwatch":        true,
		"drupal_watchdog": true,
		"ocms":            true,
	}

	for _, typ := range types {
		if !expected[typ] {
			t.Errorf("ValidSourceTypes() contains unexpected type: %s", typ)
		}
	}
}

func TestParseSourceType(t *testing.T) {
	tests := []struct {
		input   string
		want    LogSourceType
		wantErr bool
	}{
		{"logwatch", LogSourceLogwatch, false},
		{"drupal_watchdog", LogSourceDrupalWatchdog, false},
		{"ocms", LogSourceOCMS, false},
		{"invalid", "", true},
		{"", "", true},
		{"LOGWATCH", "", true}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseSourceType(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSourceType(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ParseSourceType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
