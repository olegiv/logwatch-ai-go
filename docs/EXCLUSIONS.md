# Finding Exclusions

The analyzer can instruct the LLM to ignore specific conditions during
analysis so they never appear as findings in the first place. This keeps
`systemStatus`, `summary`, `metrics`, `criticalIssues`, `warnings`, and
`recommendations` all coherent with each other — the excluded conditions
do not inflate `failedLogins`, do not push status to `Bad`, and are not
mentioned in the textual summary.

Patterns are rendered directly into the prompt sent to the LLM, which is
why they must not contain secrets.

The feature is **opt-in**: if no `exclusions.json` file is present the
analyzer behaves exactly as before (byte-identical prompt output, so
Anthropic prompt-cache hits are preserved).

## Quick Start

1. Copy the template:

   ```bash
   cp configs/exclusions.json.example configs/exclusions.json
   ```

2. Edit the file and list the conditions you want the LLM to ignore.

3. Run the analyzer. A log line like

   ```
   Injecting operator-defined exclusion patterns into prompt patterns_global=1 patterns_contextual=2
   ```

   confirms the patterns were attached to the request. Pattern text is
   never logged.

If the file is absent the feature is a silent no-op.

## File Format

```json
{
  "version": "1.1",
  "global": [
    "TLS certificate validation failures"
  ],
  "logwatch": [
    "kernel: NETDEV WATCHDOG"
  ],
  "drupal": [
    "Deprecated function"
  ],
  "sites": {
    "production": [
      "cron run exceeded the time limit"
    ],
    "staging": [
      "Email delivery delayed"
    ]
  }
}
```

| Field      | Meaning                                                                                                  |
|------------|----------------------------------------------------------------------------------------------------------|
| `version`  | Config format version. `"1.1"` (recommended) or `"1.0"` (backward-compatible, no `logwatch`/`drupal`).   |
| `global`   | Applies to every run. Rendered into the **system prompt** (stable, cache-friendly for Anthropic).        |
| `logwatch` | Applies only to logwatch runs. Rendered into the **user prompt**. (v1.1 only.)                           |
| `drupal`   | Applies only to Drupal watchdog runs, regardless of site. Rendered into the **user prompt**. (v1.1.)     |
| `sites`    | Map keyed by Drupal site ID (from `drupal-sites.json`). Stacked on top of `drupal`. User-prompt section. |

## Resolution

| Run type         | System prompt gets | User prompt gets            |
|------------------|--------------------|-----------------------------|
| Logwatch         | `global`           | `logwatch`                  |
| Drupal (site X)  | `global`           | `drupal` + `sites.X`        |

`logwatch` patterns are ignored for Drupal runs. `drupal` and
`sites.<id>` patterns are ignored for logwatch runs. Unknown site IDs
fall back to just `drupal`.

## Match Semantics

- The LLM is instructed to match **case-insensitively by substring**
  against the finding text it would otherwise emit. Regex metacharacters
  are treated as literal text.
- Scope: the instruction applies uniformly to `criticalIssues`,
  `warnings`, `recommendations`, and — crucially — to `systemStatus`,
  `summary`, and `metrics`. A fully excluded run yields a coherent
  "Good" analysis, not a "Bad" status with the findings silently removed.
- Order within a list does not change behavior. Patterns are de-duplicated
  case-insensitively at load time.

### Best-effort guarantee

Prompt-level exclusions are **advisory to the model**, not a hard filter.
In practice, Claude follows the instruction reliably, but there is no
deterministic guarantee that every excluded condition will be suppressed
on every run. Local (Ollama, LM Studio) models may be less consistent.
For regulatory or compliance contexts that require guaranteed suppression,
this feature is not the right tool.

## CLI Options

| Flag                        | Purpose                                                      |
|-----------------------------|--------------------------------------------------------------|
| `-exclusions-config <path>` | Use a specific exclusions file. Overrides auto-discovery.    |

## Auto-Discovery

When `-exclusions-config` is not given, the analyzer searches, in order:

1. `./exclusions.json`
2. `./configs/exclusions.json`
3. `/opt/logwatch-ai/exclusions.json`
4. `~/.config/logwatch-ai/exclusions.json`

The first file that exists is used. If none exist, the feature is
disabled for that run.

## Validation

`exclusions.json` is validated on load. The analyzer refuses to start if:

- `version` is missing or is anything other than `"1.0"` or `"1.1"`.
- Any pattern is blank or whitespace-only.
- The same pattern appears twice (case-insensitively) in the same list.
- Any site key is empty.
- Any single list contains more than 50 patterns.
- The file is larger than 1 MiB (sanity limit).

Errors point to the offending entry (e.g., `global[3]: pattern is blank`
or `logwatch: too many patterns`).

## Security Considerations

- **Do not put secrets into patterns.** Patterns are sent verbatim to the
  LLM provider (Anthropic, Ollama, LM Studio). Treat them as public.
- Patterns are structurally sanitized before injection via
  `ai.NormalizePromptContent`: control characters (newlines, tabs, DEL,
  ESC) are stripped or replaced with spaces so an operator typo cannot
  break the bullet-list structure, NFKC normalization collapses
  fullwidth / ligature variants to their ASCII forms, and zero-width /
  bidi characters are removed. This matches how the LLM-facing log
  content is normalized so patterns still substring-match finding text.
- Prompt-injection phrase replacement (the `[FILTERED]` behavior of
  `ai.SanitizeLogContent`) is **not** applied to operator patterns.
  Operators must be able to exclude legitimate log lines containing
  tokens like `USER:`, `SYSTEM:`, or wording such as "ignore previous
  instructions" that appear in real syslog / watchdog records. The
  containment boundary for operator patterns is the rendered bullet-list
  framing plus the "MUST NOT / treat as absent" instruction, not text
  rewriting.
- Patterns are capped at 200 runes each (longer are truncated with `...`)
  and at 50 entries per scope. The per-scope cap applies independently,
  so a Drupal run with 50 `drupal` patterns + 50 `sites.<id>` patterns
  can reach 100 contextual patterns total.
- `exclusions.json` is read once at startup from operator-controlled
  disk, with a 1 MiB size cap.
- Pattern text is never logged at info level; only counts are reported.

## Operational Notes

- Keep `global` stable across runs — it lives in the system prompt and
  is included in the Anthropic prompt-cache prefix. Modifying it
  invalidates the cache.
- Unknown site IDs in `sites` do not fail the run; patterns for sites
  that do not exist simply have no effect.
- Historical context retrieved from the summaries database may still
  mention findings that are now excluded, for up to 90 days (the
  retention window). New analyses will not reintroduce them, because the
  LLM is instructed to ignore them regardless of where they appear.
- `exclusions.json.v1.0` files continue to work unchanged. Add `logwatch`
  and `drupal` fields and bump the version to `"1.1"` to use the new
  source-wide scopes.

## When _Not_ to Use This

- To mask an emerging security incident. Exclusions exist to reduce
  known noise, not to silence alerts.
- To work around prompt issues. If the LLM repeatedly produces a wrong
  description, consider tightening the prompt or the log preprocessor
  first.
- Where strict guaranteed suppression is required. This is a best-effort
  feature by construction.
