-- id: endpoint_status_distribution
-- purpose: HTTP status code distribution per endpoint for a single run
-- description: Counts requests per endpoint/status-code pair so callers can
--   render a status distribution without scanning raw rows client-side.
--   Endpoint identity is COALESCE(NULLIF(name, ''), url); see
--   endpoint_summary.sql for why.
-- required_columns: run_id, url, name, status
-- parameters: run_id:VARCHAR
-- expected_output: endpoint, status, count

SELECT
    COALESCE(NULLIF(name, ''), url) AS endpoint,
    status,
    COUNT(*) AS count
FROM metrics
WHERE run_id = ?
GROUP BY COALESCE(NULLIF(name, ''), url), status
ORDER BY endpoint, status
