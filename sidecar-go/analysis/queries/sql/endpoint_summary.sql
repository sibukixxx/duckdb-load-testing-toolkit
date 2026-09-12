-- id: endpoint_summary
-- purpose: Per-endpoint latency and error-rate summary for a single run
-- description: Aggregates request-level metrics by endpoint (url) for one
--   run_id, returning request/error counts, latency percentiles, and average
--   network/backend timing phases. This is the primary input for endpoint
--   analysis and bottleneck evidence.
-- required_columns: run_id, url, status, error_code, rtt, dns_lookup, tcp_connect, tls_handshake, ttfb, content_transfer
-- parameters: run_id:VARCHAR
-- expected_output: endpoint, request_count, error_count, avg_rtt, p50_rtt, p90_rtt, p95_rtt, p99_rtt, max_rtt, avg_dns, avg_tcp, avg_tls, avg_ttfb, avg_transfer

SELECT
    url AS endpoint,
    COUNT(*) AS request_count,
    COUNT(CASE WHEN status >= 400 OR (error_code IS NOT NULL AND error_code != '') THEN 1 END) AS error_count,
    AVG(rtt) AS avg_rtt,
    PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY rtt) AS p50_rtt,
    PERCENTILE_CONT(0.90) WITHIN GROUP (ORDER BY rtt) AS p90_rtt,
    PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY rtt) AS p95_rtt,
    PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY rtt) AS p99_rtt,
    MAX(rtt) AS max_rtt,
    AVG(dns_lookup) AS avg_dns,
    AVG(tcp_connect) AS avg_tcp,
    AVG(tls_handshake) AS avg_tls,
    AVG(ttfb) AS avg_ttfb,
    AVG(content_transfer) AS avg_transfer
FROM metrics
WHERE run_id = ?
GROUP BY url
ORDER BY request_count DESC
