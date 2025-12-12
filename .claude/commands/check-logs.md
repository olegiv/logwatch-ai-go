Check application logs for errors and recent activity.

Execute the following steps:

1. Determine the log location:
   - Development: ./logs/analyzer.log
   - Production: /opt/logwatch-ai/logs/analyzer.log

2. Check if log file exists:
   ```bash
   ls -lh ./logs/analyzer.log
   ```

3. Show the last 50 lines:
   ```bash
   tail -50 ./logs/analyzer.log
   ```

4. Search for errors:
   ```bash
   grep -i error ./logs/analyzer.log | tail -20
   ```

5. Search for warnings:
   ```bash
   grep -i warn ./logs/analyzer.log | tail -20
   ```

6. Show recent successful runs:
   ```bash
   grep "Analysis completed successfully" ./logs/analyzer.log | tail -10
   ```

7. Check log rotation status:
   ```bash
   ls -lh ./logs/
   ```

8. Provide analysis:
   - Report any errors found
   - Note the timestamp of last run
   - Identify any patterns or recurring issues
   - Suggest follow-up actions if problems detected

Logs rotate automatically at 10MB. Multiple log files indicate rotation has occurred.
