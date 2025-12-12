Show database statistics and recent analysis summaries.

Execute the following steps:

1. Check database file:
   ```bash
   ls -lh ./data/summaries.db
   ```

2. Show total records and basic stats:
   ```bash
   sqlite3 ./data/summaries.db "
   SELECT
       COUNT(*) AS total_summaries,
       MIN(timestamp) AS first_analysis,
       MAX(timestamp) AS last_analysis,
       ROUND(SUM(cost_usd), 4) AS total_cost_usd
   FROM summaries;"
   ```

3. Show status distribution:
   ```bash
   sqlite3 -header -column ./data/summaries.db "
   SELECT
       system_status,
       COUNT(*) AS count,
       ROUND(AVG(cost_usd), 6) AS avg_cost
   FROM summaries
   GROUP BY system_status
   ORDER BY count DESC;"
   ```

4. Show last 10 analyses:
   ```bash
   sqlite3 -header -column ./data/summaries.db "
   SELECT
       id,
       datetime(timestamp, 'localtime') AS time,
       system_status AS status,
       substr(summary, 1, 60) || '...' AS summary,
       ROUND(cost_usd, 6) AS cost
   FROM summaries
   ORDER BY timestamp DESC
   LIMIT 10;"
   ```

5. Show cost statistics:
   ```bash
   sqlite3 ./data/summaries.db "
   SELECT
       'Avg Cost' AS metric, ROUND(AVG(cost_usd), 6) AS value FROM summaries
   UNION ALL
   SELECT 'Min Cost', ROUND(MIN(cost_usd), 6) FROM summaries
   UNION ALL
   SELECT 'Max Cost', ROUND(MAX(cost_usd), 6) FROM summaries
   UNION ALL
   SELECT 'Total Cost', ROUND(SUM(cost_usd), 4) FROM summaries;"
   ```

6. Provide analysis:
   - Summarize the data
   - Note any concerning patterns
   - Suggest actions if needed (cleanup, cost optimization, etc.)

Database stores all analysis history with automatic cleanup after 90 days.
