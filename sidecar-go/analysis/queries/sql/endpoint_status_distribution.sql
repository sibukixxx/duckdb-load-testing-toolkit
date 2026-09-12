-- id: endpoint_status_distribution
-- purpose: HTTP status code distribution per endpoint for a single run
-- description: Counts requests per endpoint/status-code pair so callers can
--   render a status distribution without scanning raw rows client-side.
-- required_columns: run_id, url, status
-- parameters: run_id:VARCHAR
-- expected_output: endpoint, status, count

SELECT
    url AS endpoint,
    status,
    COUNT(*) AS count
FROM metrics
WHERE run_id = ?
GROUP BY url, status
ORDER BY url, status
