Generate a comprehensive cost report for Claude AI usage.

Execute the following steps:

1. Show total costs all-time:
   ```bash
   sqlite3 ./data/summaries.db "
   SELECT
       COUNT(*) AS total_analyses,
       SUM(input_tokens) AS total_input_tokens,
       SUM(output_tokens) AS total_output_tokens,
       ROUND(SUM(cost_usd), 4) AS total_cost_usd,
       ROUND(AVG(cost_usd), 6) AS avg_cost_per_run
   FROM summaries;"
   ```

2. Show costs by month:
   ```bash
   sqlite3 -header -column ./data/summaries.db "
   SELECT
       strftime('%Y-%m', timestamp) AS month,
       COUNT(*) AS runs,
       ROUND(SUM(cost_usd), 4) AS total,
       ROUND(AVG(cost_usd), 6) AS avg
   FROM summaries
   GROUP BY strftime('%Y-%m', timestamp)
   ORDER BY month DESC;"
   ```

3. Show daily costs for last 30 days:
   ```bash
   sqlite3 -header -column ./data/summaries.db "
   SELECT
       DATE(timestamp) AS date,
       COUNT(*) AS runs,
       ROUND(SUM(cost_usd), 6) AS cost
   FROM summaries
   WHERE timestamp >= datetime('now', '-30 days')
   GROUP BY DATE(timestamp)
   ORDER BY date DESC;"
   ```

4. Calculate projections:
   ```bash
   sqlite3 ./data/summaries.db "
   SELECT
       'Projected Monthly' AS period,
       ROUND(SUM(cost_usd) / 30.0 * 30, 4) AS cost_usd
   FROM summaries
   WHERE timestamp >= datetime('now', '-30 days')
   UNION ALL
   SELECT
       'Projected Yearly',
       ROUND(SUM(cost_usd) / 30.0 * 365, 2)
   FROM summaries
   WHERE timestamp >= datetime('now', '-30 days');"
   ```

5. Identify expensive runs (top 10):
   ```bash
   sqlite3 -header -column ./data/summaries.db "
   SELECT
       datetime(timestamp, 'localtime') AS time,
       input_tokens,
       output_tokens,
       ROUND(cost_usd, 6) AS cost
   FROM summaries
   ORDER BY cost_usd DESC
   LIMIT 10;"
   ```

6. Provide analysis and recommendations:
   - Compare to expected costs ($0.011-0.022 per run)
   - Note any concerning trends
   - Suggest optimizations if costs are high
   - Verify prompt caching is working (check logs)

Expected costs:
- Daily: $0.011-0.022
- Monthly: ~$0.47
- Yearly: ~$5.64
