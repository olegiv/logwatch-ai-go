Build the logwatch-analyzer binary for development.

Execute the following steps:

1. Build the application using the Makefile:
   ```bash
   make build
   ```

2. Verify the build:
   - Check that bin/logwatch-analyzer was created
   - Report the binary size
   - Confirm it's executable

3. Show the binary details:
   ```bash
   ls -lh bin/logwatch-analyzer
   file bin/logwatch-analyzer
   ```

4. Provide next steps:
   - How to run: ./bin/logwatch-analyzer
   - How to test: make run (builds and runs immediately)
   - Production build: make build-prod (smaller, optimized binary)

This creates a development build with:
- Verbose output (-v)
- Debug symbols included
- Not optimized (faster compilation)

For production deployment, use `make build-prod` instead.
