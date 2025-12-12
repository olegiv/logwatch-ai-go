Run all tests for the logwatch-ai-go project.

Execute the following steps:

1. Run the test suite using the Makefile target:
   ```bash
   make test
   ```

2. Analyze the test results:
   - Report number of tests run
   - Highlight any failures
   - Show which packages were tested

3. If any tests fail:
   - Identify the failing test(s)
   - Show the error messages
   - Suggest potential fixes based on the error

4. Report test coverage summary if available

This command runs all unit tests across all packages:
- internal/ai (Claude client, prompt generation)
- internal/config (configuration loading and validation)
- internal/errors (error sanitization)
- internal/logging (secure logging)
- internal/logwatch (log reading, preprocessing, token estimation)
- internal/notification (Telegram formatting)
- internal/storage (SQLite operations)
