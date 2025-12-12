Prepare a complete deployment package for Linux production servers.

Execute the following steps:

1. Clean and build for Linux:
   ```bash
   make clean
   make build-linux-amd64
   ```

2. Create deployment directory:
   ```bash
   mkdir -p logwatch-ai-deploy
   ```

3. Copy necessary files:
   ```bash
   cp bin/logwatch-analyzer-linux-amd64 logwatch-ai-deploy/logwatch-analyzer
   cp -r scripts logwatch-ai-deploy/
   cp configs/.env.example logwatch-ai-deploy/
   ```

4. Set correct permissions:
   ```bash
   chmod +x logwatch-ai-deploy/logwatch-analyzer
   chmod +x logwatch-ai-deploy/scripts/*.sh
   ```

5. Generate checksums:
   ```bash
   cd logwatch-ai-deploy
   shasum -a 256 logwatch-analyzer scripts/*.sh > checksums.txt
   cd ..
   ```

6. Create deployment tarball:
   ```bash
   tar -czf logwatch-ai-deploy.tar.gz logwatch-ai-deploy/
   ```

7. Generate final checksum:
   ```bash
   shasum -a 256 logwatch-ai-deploy.tar.gz
   ```

8. Show deployment package details:
   ```bash
   ls -lh logwatch-ai-deploy.tar.gz
   tar -tzf logwatch-ai-deploy.tar.gz
   ```

9. Provide deployment instructions:
   - Transfer: `scp logwatch-ai-deploy.tar.gz user@server:/tmp/`
   - Extract: `tar -xzf logwatch-ai-deploy.tar.gz`
   - Install: `sudo cp logwatch-ai-deploy/logwatch-analyzer /opt/logwatch-ai/`
   - Configure: Edit `/opt/logwatch-ai/.env` with production credentials
   - Test: Run `/opt/logwatch-ai/logwatch-analyzer` manually
   - Schedule: Add to cron or systemd timer

Deployment package is ready for transfer to production server(s).
