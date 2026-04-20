# Finding Exclusions

The analyzer can suppress individual findings returned by the LLM before
they reach SQLite or Telegram. This is intended for known-and-accepted
noise — for example, a benign "TLS certificate validation failures"
message coming from an internal host that the team has already triaged.

The feature is **opt-in**: if no `exclusions.json` file is present the
analyzer behaves exactly as before.

## Quick Start

1. Copy the template:

   ```bash
   cp configs/exclusions.json.example configs/exclusions.json
   ```

2. Edit the file and list the descriptions you want suppressed.

3. Run the analyzer. A log line like

   ```
   Applied finding exclusions critical_excluded=1 warnings_excluded=0 recommendations_excluded=0
   ```

   confirms the filter ran.

If the file is absent the feature is a silent no-op.

## File Format

```json
{
  "version": "1.0",
  "global": [
    "TLS certificate validation failures"
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

| Field     | Meaning                                                                                     |
|-----------|---------------------------------------------------------------------------------------------|
| `version` | Config format version. Must be `"1.0"` today. Required.                                     |
| `global`  | Patterns applied to every run, regardless of log source.                                    |
| `sites`   | Optional map keyed by Drupal site ID (from `drupal-sites.json`). Stacked on top of `global`.|

The `sites` map has no effect for the `logwatch` source type because it
does not have a site ID.

## Match Semantics

- **Case-insensitive substring**. A pattern matches a finding when it
  appears anywhere in the finding text after both sides are lowercased.
  `"tls certificate"` matches `"TLS certificate validation failures on
  host alpha"`.
- **Regex metacharacters are inert**. `.` only matches a literal dot,
  `.*` only matches the literal characters `.*`, and so on. This is
  deliberate: plain substring matching means operator-authored patterns
  cannot cause catastrophic backtracking (no ReDoS).
- **Scope**: applied uniformly to `criticalIssues`, `warnings`, and
  `recommendations`. There is no per-pattern category targeting.
- **Order does not matter**. Patterns are de-duplicated case-insensitively
  and applied as a set.

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

## Behavior When All Findings Are Excluded

The analysis is still stored and still sent to Telegram. The only change
is that the matching bullets are missing from the message. The overall
`systemStatus`, `summary`, and metrics are **not** re-evaluated — the
LLM's original verdict stands, so an operator can still see that the run
saw issues, even if all of them were suppressed.

## Validation

`exclusions.json` is validated on load. The analyzer refuses to start if:

- `version` is missing.
- Any pattern is blank or whitespace-only.
- The same pattern appears twice (case-insensitively) in the same list.
- Any site key is empty.
- The file is larger than 1 MiB (sanity limit).

Errors point to the offending entry (e.g., `global[3]: pattern is blank`).

## Operational Notes

- Pattern text is **not** logged at info level to avoid leaking
  operator-authored strings into log aggregation. The log line only
  reports numeric counts.
- `exclusions.json` has the same trust level as `.env`: operator-
  authored, on-disk, loaded once per run. Do not fetch patterns from the
  network or from environment variables at runtime.
- Unknown site IDs in `sites` do not fail the run; patterns for sites
  that do not exist simply have no effect.

## When _Not_ to Use This

- To mask an emerging security incident. Exclusions exist to reduce
  known noise, not to silence alerts.
- To work around prompt issues. If the LLM repeatedly produces a wrong
  description, consider tightening the prompt or the log preprocessor
  first.
