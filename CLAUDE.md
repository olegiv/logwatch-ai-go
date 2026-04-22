# CLAUDE.md

This file provides guidance to Claude Code when working with this repository.

## Project Overview

Logwatch AI Analyzer is an intelligent system log analyzer that uses LLM to analyze log reports and send actionable insights via Telegram. Go port optimized for single-binary deployment.

**Log Sources:** Logwatch (Linux), Drupal Watchdog (PHP/Drupal)
**LLM Providers:** Anthropic Claude, Ollama, LM Studio
**Key Tech:** Go 1.25+, pure Go SQLite (modernc.org/sqlite), Telegram Bot API

**Status:** Production ready - deployed to Integration, QA, and Pre-Production on Debian 12.

## Build Commands

```bash
# Development
make build          # Dev build
make test           # Run all tests
make test-coverage  # Coverage report (opens coverage.html)
make fmt && make vet # Format and lint

# Production
make build-prod           # Optimized build for current platform
make build-linux-amd64    # Linux AMD64 (Debian 12/Ubuntu)
make build-all-platforms  # All platforms
make install              # Install to /opt/logwatch-ai (requires sudo)
```

## Package Structure

```
cmd/analyzer/       - Main entry point, prompt fitting (prompt_fit.go)
internal/
  ├── ai/          - LLM clients (Anthropic, Ollama, LM Studio), prompts, parsing, token counting
  ├── analyzer/    - Multi-source interfaces, token budget calculation (budget.go)
  ├── config/      - Configuration loading (viper + .env)
  ├── drupal/      - Drupal watchdog reader, preprocessor, prompts
  ├── errors/      - Error sanitization (credential redaction)
  ├── exclusions/  - Finding exclusion config loader and prompt-injection renderer
  ├── logging/     - Secure logger wrapper (credential filtering)
  ├── logwatch/    - Logwatch reader, preprocessing, token estimation
  ├── notification/- Telegram client and message formatting
  └── storage/     - SQLite operations (summaries table)
scripts/           - Shell scripts (install.sh, generate-*.sh)
configs/           - Configuration templates (.env.example, drupal-sites.json.example)
testdata/          - Test fixtures
docs/              - Extended documentation (DEPLOYMENT.md, COST_OPTIMIZATION.md)
```

## Key Interfaces

```go
// internal/analyzer/interfaces.go - Implement these for new log sources
type LogReader interface {
    Read(sourcePath string) (string, error)
    Validate(content string) error
    GetSourceInfo(sourcePath string) (map[string]interface{}, error)
}

type Preprocessor interface {
    EstimateTokens(content string) int
    Process(content string) (string, error)
    ShouldProcess(content string, maxTokens int) bool
}

// Optional: dynamic token budget support (internal/analyzer/interfaces.go)
type BudgetPreprocessor interface {
    ProcessWithBudget(content string, maxTokens int) (string, error)
}

type PromptBuilder interface {
    // globalExclusions: operator-scoped patterns rendered into the system
    // prompt. Pass nil for byte-identical output vs the no-exclusions case
    // (preserves Anthropic prompt-cache hits).
    GetSystemPrompt(globalExclusions []string) string

    // contextualExclusions: run-scoped patterns (source-wide and/or
    // site-specific) rendered into the user prompt. Pass nil for no
    // exclusions. logContent should already be sanitized before passing.
    GetUserPrompt(logContent, historicalContext string, contextualExclusions []string) string

    GetLogType() string
}
```

## Configuration

**LLM Provider Settings:**
- `LLM_PROVIDER`: `anthropic` (default), `ollama`, or `lmstudio`
- Anthropic: `ANTHROPIC_API_KEY` (must start with `sk-ant-`), `CLAUDE_MODEL`
- Ollama: `OLLAMA_BASE_URL`, `OLLAMA_MODEL` (default: `llama3.3:latest`)
- LM Studio: `LMSTUDIO_BASE_URL`, `LMSTUDIO_MODEL` (default: `local-model`)

**Telegram:** `TELEGRAM_BOT_TOKEN` (format: `number:token`), `TELEGRAM_CHANNEL_ARCHIVE_ID` (must be < -100)

**Log Sources:**
- `LOG_SOURCE_TYPE`: `logwatch` or `drupal_watchdog`
- Logwatch: `LOGWATCH_OUTPUT_PATH` required
- Drupal: requires `drupal-sites.json` and `jq` installed

**Multi-Site Drupal:** Uses `drupal-sites.json` for centralized site configuration.
- CLI: `-drupal-site <id>`, `-drupal-sites-config <path>`, `-list-drupal-sites`
- Search locations: `./`, `./configs/`, `/opt/logwatch-ai/`, `~/.config/logwatch-ai/`

**Finding Exclusions (optional):** `exclusions.json` instructs the LLM to ignore specific conditions so they never appear as findings and do not influence `systemStatus`, `summary`, or `metrics`. Patterns are injected into the prompt, not post-filtered.
- CLI: `-exclusions-config <path>` (auto-discovery uses the same search locations as `drupal-sites.json`)
- Schema v1.1: `global` (system prompt, cache-friendly), `logwatch` (logwatch user prompts), `drupal` (all Drupal user prompts), `sites.<id>` (per-Drupal-site, stacked on top of `drupal`). v1.0 files without `logwatch`/`drupal` continue to load.
- Resolution: logwatch runs use `global` + `logwatch`; Drupal site X uses `global` + `drupal` + `sites.X`.
- LLM enforcement is best-effort, not a hard filter. Patterns are sent verbatim to the provider — do not put secrets in them.
- Template: `configs/exclusions.json.example`; full docs: `docs/EXCLUSIONS.md`

## Preprocessing & Prompt Fitting

When logs exceed the available token budget:
1. Split by `###` headers
2. Classify priority: HIGH (ssh, security, auth, error), MEDIUM (network, disk, warning), LOW (rest)
3. Deduplicate similar lines
4. Progressive compression profiles (100/50/20% → 85/35/10% → 70/20/5% → 50/10/2%)
5. Aggressive compression (essential lines only) and hard truncation as final fallbacks

**Prompt fitting** (`cmd/analyzer/prompt_fit.go`):
- **Anthropic**: Uses exact token counting API (`CountPromptTokens`) with iterative recompression (up to 4 attempts)
- **Other providers**: Heuristic budget calculation based on estimated token counts (`internal/analyzer/budget.go`)

Token estimation: `max(chars/4, words/0.75)`

## Database Schema

```sql
CREATE TABLE summaries (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT NOT NULL,
    log_source_type TEXT NOT NULL DEFAULT 'logwatch',
    site_name TEXT NOT NULL DEFAULT '',
    system_status TEXT NOT NULL,  -- Good/Warning/Critical/Bad
    summary TEXT NOT NULL,
    critical_issues TEXT,         -- JSON array
    warnings TEXT,                -- JSON array
    recommendations TEXT,         -- JSON array
    metrics TEXT,                 -- JSON object
    input_tokens INTEGER,
    output_tokens INTEGER,
    cost_usd REAL
);
```

Auto-migrates from v1 to v2. Cleanup: 90 days retention.

## Error Handling

- Graceful degradation: Missing historical context = warning, not failure
- Database/cleanup errors = warning (notification still succeeds)
- Fail fast on: config validation, file reading, LLM API, Telegram send

## Code Style

- Use SecureLogger: `log.Info().Str("key", value).Msg("message")`
- Credential errors: `internalerrors.Wrapf(err, "failed to X")`
- Other errors: `fmt.Errorf("failed to X: %w", err)`
- Constants for exit codes, timeouts, retry counts
- Defer cleanup: `defer store.Close()`, `defer telegramClient.Close()`

**License Headers:** All `.go` source files must include the SPDX short form header:
```go
// Copyright (c) 2025-2026 Oleg Ivanchenko
// SPDX-License-Identifier: GPL-3.0-or-later

package <name>
```

## Adding a New Log Source

1. Create package in `internal/<source>/`
2. Implement `LogReader`, `Preprocessor`, `PromptBuilder` from `internal/analyzer/interfaces.go`
3. Add source type constant to `internal/analyzer/registry.go`
4. Add config fields to `internal/config/config.go`
5. Add factory case in `cmd/analyzer/main.go:createLogSource()`
6. Add tests and fixtures in `testdata/<source>/`

## Common Issues

**"Database is locked"**: Ensure single instance, check file permissions, 5s busy timeout handles temporary locks.

**"Token estimation off"**: Calibrated for English text, may underestimate 10-20% for non-English.

**"Preprocessing removes too much"**: Increase `MAX_PREPROCESSING_TOKENS`, modify priority keywords in `preprocessor.go`.

**"Claude API timeouts"**: Default 120s, increase preprocessing for large logs.

**Drupal "Invalid JSON"**: Validate with `jq . /path/to/watchdog.json`, check UTF-8 encoding.

## Extended Documentation

- **Deployment**: See `docs/DEPLOYMENT.md`
- **Cost Optimization**: See `docs/COST_OPTIMIZATION.md`
- **Cron Setup**: See `docs/CRON_SETUP.md`
- **Troubleshooting**: See `docs/TROUBLESHOOTING.md`
- **Finding Exclusions**: See `docs/EXCLUSIONS.md`

## Claude Code Extensions

Specialized agents and slash commands are defined in `.claude/agents/` and `.claude/commands/`. Key commands:

- `/test` - Run all tests
- `/build` - Development build
- `/build-all` - All platform builds
- `/db-stats` - Database statistics
- `/cost-report` - Cost analysis
- `/security-audit` - Security scan
- `/code-quality` - Code quality scan
