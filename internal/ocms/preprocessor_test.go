package ocms

import (
	"strings"
	"testing"

	"github.com/olegiv/logwatch-ai-go/internal/analyzer"
)

var _ analyzer.Preprocessor = (*Preprocessor)(nil)
var _ analyzer.BudgetPreprocessor = (*Preprocessor)(nil)

func TestPreprocessor_Basic(t *testing.T) {
	t.Parallel()

	p := NewPreprocessor(1000)
	if p == nil {
		t.Fatal("NewPreprocessor returned nil")
	}

	content := strings.Repeat("INFO request completed in 15ms\n", 200)
	if p.EstimateTokens(content) <= 0 {
		t.Fatal("EstimateTokens should be > 0")
	}

	processed, err := p.Process(content)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if processed == "" {
		t.Fatal("Process() should not return empty content")
	}
}
