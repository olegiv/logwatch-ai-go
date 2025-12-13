package analyzer

import (
	"fmt"
	"sync"
)

// LogSourceType identifies the type of log source.
type LogSourceType string

// Supported log source types.
const (
	LogSourceLogwatch       LogSourceType = "logwatch"
	LogSourceDrupalWatchdog LogSourceType = "drupal_watchdog"
)

// LogSource bundles all components needed to analyze a specific log type.
type LogSource struct {
	Type          LogSourceType
	Reader        LogReader
	Preprocessor  Preprocessor
	PromptBuilder PromptBuilder
}

// Registry holds all registered log sources.
// It provides thread-safe access to log source configurations.
type Registry struct {
	mu      sync.RWMutex
	sources map[LogSourceType]*LogSource
}

// NewRegistry creates a new empty registry.
func NewRegistry() *Registry {
	return &Registry{
		sources: make(map[LogSourceType]*LogSource),
	}
}

// Register adds a log source to the registry.
// If a source with the same type already exists, it will be overwritten.
func (r *Registry) Register(source *LogSource) error {
	if source == nil {
		return fmt.Errorf("cannot register nil log source")
	}
	if source.Type == "" {
		return fmt.Errorf("log source type cannot be empty")
	}
	if source.Reader == nil {
		return fmt.Errorf("log source reader cannot be nil")
	}
	if source.Preprocessor == nil {
		return fmt.Errorf("log source preprocessor cannot be nil")
	}
	if source.PromptBuilder == nil {
		return fmt.Errorf("log source prompt builder cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.sources[source.Type] = source
	return nil
}

// Get retrieves a log source by type.
// Returns nil and false if the source type is not registered.
func (r *Registry) Get(sourceType LogSourceType) (*LogSource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	source, ok := r.sources[sourceType]
	return source, ok
}

// MustGet retrieves a log source by type or panics if not found.
// Use this only when you're certain the source type is registered.
func (r *Registry) MustGet(sourceType LogSourceType) *LogSource {
	source, ok := r.Get(sourceType)
	if !ok {
		panic(fmt.Sprintf("log source type %q not registered", sourceType))
	}
	return source
}

// List returns all registered log source types.
func (r *Registry) List() []LogSourceType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]LogSourceType, 0, len(r.sources))
	for t := range r.sources {
		types = append(types, t)
	}
	return types
}

// Has checks if a log source type is registered.
func (r *Registry) Has(sourceType LogSourceType) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.sources[sourceType]
	return ok
}

// ValidSourceTypes returns a list of valid log source type strings.
// Useful for configuration validation.
func ValidSourceTypes() []string {
	return []string{
		string(LogSourceLogwatch),
		string(LogSourceDrupalWatchdog),
	}
}

// ParseSourceType converts a string to LogSourceType.
// Returns an error if the string is not a valid source type.
func ParseSourceType(s string) (LogSourceType, error) {
	switch s {
	case string(LogSourceLogwatch):
		return LogSourceLogwatch, nil
	case string(LogSourceDrupalWatchdog):
		return LogSourceDrupalWatchdog, nil
	default:
		return "", fmt.Errorf("invalid log source type: %q (valid types: %v)", s, ValidSourceTypes())
	}
}
