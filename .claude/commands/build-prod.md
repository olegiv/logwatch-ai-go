Build optimized production binary for the current platform.

Execute the following steps:

1. Build production-optimized binary:
   ```bash
   make build-prod
   ```

2. Verify the build:
   - Check bin/logwatch-analyzer exists
   - Report binary size (should be 30-40% smaller than dev build)
   - Show optimization flags used

3. Compare to development build if it exists:
   ```bash
   ls -lh bin/logwatch-analyzer*
   ```

4. Explain the optimizations:
   - `-ldflags="-s -w"`: Strip symbols and debug info
   - `-trimpath`: Remove file system paths
   - Result: Smaller binary, better security, production-ready

5. Provide deployment guidance:
   - This binary is for the current platform only
   - For Linux deployment: use `make build-linux-amd64`
   - For macOS deployment: use `make build-darwin-arm64`
   - To install: use `make install` (requires sudo)

Production binary is suitable for deployment but should be tested first.
