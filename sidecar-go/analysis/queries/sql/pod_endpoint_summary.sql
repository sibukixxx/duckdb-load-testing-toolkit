-- id: pod_endpoint_summary
-- purpose: Per-pod, per-endpoint latency summary for a single run
-- description: Aggregates metrics by pod_id and endpoint so outlier pods
--   (a single worker performing much worse than its peers) can be detected
--   for a given endpoint.
-- required_columns: run_id, pod_id, url, status, error_code, rtt
-- parameters: run_id:VARCHAR
-- expected_output: endpoint, pod_id, request_count, error_count, avg_rtt, p95_rtt

SELECT
    url AS endpoint,
    pod_id,
    COUNT(*) AS request_count,
    COUNT(CASE WHEN status >= 400 OR (error_code IS NOT NULL AND error_code != '') THEN 1 END) AS error_count,
    AVG(rtt) AS avg_rtt,
    PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY rtt) AS p95_rtt
FROM metrics
WHERE run_id = ?
GROUP BY url, pod_id
ORDER BY url, pod_id
