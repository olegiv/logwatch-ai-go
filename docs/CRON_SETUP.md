# Cron Setup Guide

logwatch-ai uses a single shell script — `run-cron.sh` — that you customize
for your host, plus **one** cron entry that calls it. Adding or removing a
site is a one-line edit.

## Quick start

1. Copy the template into the install directory and make it executable:
   ```bash
   sudo cp /opt/logwatch-ai/scripts/run-cron.sh.example /opt/logwatch-ai/run-cron.sh
   sudo chmod 755 /opt/logwatch-ai/run-cron.sh
   ```

2. Edit `run-cron.sh` and uncomment / fill in the lines for your sources:
   ```bash
   sudo $EDITOR /opt/logwatch-ai/run-cron.sh
   ```
   - **logwatch** (system source): one job pair (generate → analyze).
     Suffix the lines with `|| exit` to gate the rest of the run on
     logwatch success.
   - **drupal**: per site listed in `drupal-sites.json` — one
     `generate-drupal-watchdog.sh` line and one analyzer line.
   - **ocms**: per site listed in `ocms-sites.json` — one analyzer line.
     Reads `ocms.log.1` (yesterday's rotated log) by default; pass
     `-ocms-range today` to read the live log.

3. Add **one** cron entry (root cron — logwatch needs `/var/log/*` access):
   ```bash
   sudo crontab -e
   ```
   Add:
   ```cron
   #@desc: Logwatch AI
   7 2 * * * /opt/logwatch-ai/run-cron.sh >> /opt/logwatch-ai/logs/cron.log 2>&1
   ```
   Or in `/etc/cron.d/logwatch-ai`:
   ```
   7 2 * * * root /opt/logwatch-ai/run-cron.sh >> /opt/logwatch-ai/logs/cron.log 2>&1
   ```

## How it works

`run-cron.sh` is a thin wrapper:

- One `run_job "tag" cmd...` per task.
- Each job is independent — a failure on one doesn't abort the rest. Append
  `|| exit` to make a job act as a gate.
- A `flock` lockfile prevents overlap if a previous run is still going.
- All output is timestamped and prefixed with `[source/site]` tags.
- Exit code = number of failed jobs (0 = success).

## Sample log output

A box with logwatch + 2 drupal sites + 3 ocms sites runs ~9 jobs:

```
2026-04-27 02:07:00 begin run on prod-01 (user=root)
2026-04-27 02:07:00 [logwatch/generate] start
2026-04-27 02:07:42 [logwatch/generate] ok
2026-04-27 02:07:42 [logwatch/analyze] start
2026-04-27 02:08:08 [logwatch/analyze] ok
2026-04-27 02:08:08 [drupal/italy/generate] start
...
2026-04-27 02:18:31 done — 9/9 ok, 0 failed
```

## Email on failure

Add `MAILTO` at the top of the crontab (or above the cron line in
`/etc/cron.d/`):

```cron
MAILTO=admin@example.com

7 2 * * * /opt/logwatch-ai/run-cron.sh >> /opt/logwatch-ai/logs/cron.log 2>&1
```

`run-cron.sh` exits non-zero only when at least one job failed, so cron
mails you on real problems only.

## Schedule customization

Edit the cron line directly:

- Twice daily: `7 2,14 * * * ...`
- Weekly (Sunday 03:07): `7 3 * * 0 ...`

## Troubleshooting

**Cron isn't firing**

```bash
sudo systemctl status cron       # Debian/Ubuntu
sudo grep CRON /var/log/syslog | tail -20
```

**A job failed**

`run-cron.sh` logs `[<source>/<site>] FAILED rc=<n>` and continues. Repro
manually:

```bash
cd /opt/logwatch-ai
./logwatch-analyzer -source-type ocms -ocms-site <id>
```

**Lockfile stuck after a crash**

```bash
sudo rm /var/lock/logwatch-ai-cron.lock
```

## Security

- Cron runs as root because logwatch needs `/var/log/*` access. Drop
  privileges per-job inside `run-cron.sh` if your site setup demands it.
- `.env` should be `chmod 600`.
- Logs go to `$INSTALL_DIR/logs/cron.log` (700 directory).

## See also

- [README.md](../README.md)
- [TROUBLESHOOTING.md](TROUBLESHOOTING.md)
- `scripts/run-cron.sh.example` — the template
