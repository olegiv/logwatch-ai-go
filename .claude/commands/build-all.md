Build optimized binaries for all supported platforms.

Execute the following steps:

1. Clean previous builds:
   ```bash
   make clean
   ```

2. Build for all platforms:
   ```bash
   make build-all-platforms
   ```

3. This will create:
   - bin/logwatch-analyzer-linux-amd64 (for Debian 12, Ubuntu 24, most Linux servers)
   - bin/logwatch-analyzer-darwin-arm64 (for macOS Apple Silicon M1/M2/M3)

4. Show all built binaries with sizes:
   ```bash
   ls -lh bin/logwatch-analyzer-*
   ```

5. Verify each binary:
   ```bash
   file bin/logwatch-analyzer-*
   ```

6. Generate checksums for deployment:
   ```bash
   shasum -a 256 bin/logwatch-analyzer-* > bin/checksums.txt
   cat bin/checksums.txt
   ```

7. Provide deployment guidance:
   - Linux binary: Transfer to Debian/Ubuntu server and install
   - macOS binary: For local testing or macOS deployment
   - Checksums: Use to verify file integrity after transfer

All binaries are production-optimized with:
- Stripped symbols (-s -w)
- Trimmed paths (-trimpath)
- Pure Go (no CGO dependencies)
- Ready for deployment
