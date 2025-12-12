Clean build artifacts and temporary files.

Execute the following steps:

1. Run the clean target:
   ```bash
   make clean
   ```

2. Verify cleanup:
   ```bash
   ls -la bin/ 2>/dev/null || echo "bin/ directory removed"
   ls -la coverage.out coverage.html 2>/dev/null || echo "Coverage files removed"
   ```

3. Show what was cleaned:
   - bin/ directory (all compiled binaries)
   - coverage.out (test coverage data)
   - coverage.html (coverage HTML report)

4. Report disk space freed (if significant)

5. Provide next steps:
   - Rebuild with: `make build` or `make build-prod`
   - Run tests: `make test`
   - Generate new coverage: `make test-coverage`

This is useful when:
- Starting fresh after build issues
- Reclaiming disk space
- Preparing for a clean rebuild
- Switching between branches

Note: This only removes build artifacts, not source code or configuration.
Database (./data/) and logs (./logs/) are preserved.
