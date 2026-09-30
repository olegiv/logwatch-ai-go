Prepare and validate a deployment using the repository's canonical tooling.

1. Run the complete local quality gate:
   ```bash
   make check
   ```

2. Build the Linux artifact:
   ```bash
   make build-linux-amd64
   ```

3. For a new host, transfer the repository plus the verified artifact and run
   the root-owned bootstrap installer:
   ```bash
   sudo BINARY_PATH=/absolute/path/to/bin/logwatch-analyzer-linux-amd64 \
     SERVICE_USER=logwatch-ai ./scripts/install.sh
   ```

4. For every existing installation, use the transactional deployment flow:
   ```bash
   make deploy-stage
   make deploy
   ```

5. If post-deploy verification fails, use:
   ```bash
   make rollback
   ```

Never overwrite `/opt/logwatch-ai/logwatch-analyzer` with `cp`: upgrades must
retain the immutable predecessor, atomic live symlink, transaction journal,
lock, smoke test, and rollback record provided by `deploy/`.

Report the artifact checksum, target host, staged version, verification result,
and whether the deployment was staged, installed, or rolled back. Do not print
credential values from `.env`; list only variable names when diagnostics need
to confirm configuration presence.
