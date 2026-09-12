package analysis

import (
	"database/sql"
	"fmt"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/queries"
)

// EndpointStats holds per-endpoint request-level statistics for a single
// run: counts, latency percentiles, and average network/backend timing
// phases (DNS/TCP/TLS/TTFB/transfer), plus a status code distribution.
type EndpointStats struct {
	Endpoint           string
	RequestCount       int64
	ErrorCount         int64
	ErrorRate          float64
	AvgRTT             float64
	P50RTT             float64
	P90RTT             float64
	P95RTT             float64
	P99RTT             float64
	MaxRTT             float64
	AvgDNS             float64
	AvgTCP             float64
	AvgTLS             float64
	AvgTTFB            float64
	AvgTransfer        float64
	StatusDistribution map[string]int64
}

// EndpointAnalyzer computes endpoint-level analysis for a run using the
// catalog SQL queries (analysis/queries), so the query text stays a single
// documented artifact shared with the validator and the CLI.
type EndpointAnalyzer struct {
	db *sql.DB
}

// NewEndpointAnalyzer creates an EndpointAnalyzer over db.
func NewEndpointAnalyzer(db *sql.DB) *EndpointAnalyzer {
	return &EndpointAnalyzer{db: db}
}

// GetEndpointStats returns per-endpoint statistics for runID, ordered by
// request count (as the underlying catalog query orders it).
func (a *EndpointAnalyzer) GetEndpointStats(runID string) ([]EndpointStats, error) {
	summary := queries.MustGet(queries.EndpointSummary)
	rows, err := a.db.Query(summary.SQL, runID)
	if err != nil {
		return nil, fmt.Errorf("endpoint analysis: %s failed: %w", summary.ID, err)
	}
	defer rows.Close()

	byEndpoint := map[string]*EndpointStats{}
	var order []string
	for rows.Next() {
		s := EndpointStats{StatusDistribution: map[string]int64{}}
		if err := rows.Scan(
			&s.Endpoint, &s.RequestCount, &s.ErrorCount,
			&s.AvgRTT, &s.P50RTT, &s.P90RTT, &s.P95RTT, &s.P99RTT, &s.MaxRTT,
			&s.AvgDNS, &s.AvgTCP, &s.AvgTLS, &s.AvgTTFB, &s.AvgTransfer,
		); err != nil {
			return nil, fmt.Errorf("endpoint analysis: scan failed: %w", err)
		}
		if s.RequestCount > 0 {
			s.ErrorRate = float64(s.ErrorCount) / float64(s.RequestCount) * 100
		}
		byEndpoint[s.Endpoint] = &s
		order = append(order, s.Endpoint)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	dist := queries.MustGet(queries.EndpointStatusDistribution)
	distRows, err := a.db.Query(dist.SQL, runID)
	if err != nil {
		return nil, fmt.Errorf("endpoint analysis: %s failed: %w", dist.ID, err)
	}
	defer distRows.Close()

	for distRows.Next() {
		var endpoint string
		var status int
		var count int64
		if err := distRows.Scan(&endpoint, &status, &count); err != nil {
			return nil, fmt.Errorf("endpoint analysis: scan status distribution failed: %w", err)
		}
		if s, ok := byEndpoint[endpoint]; ok {
			s.StatusDistribution[fmt.Sprintf("%d", status)] = count
		}
	}
	if err := distRows.Err(); err != nil {
		return nil, err
	}

	out := make([]EndpointStats, 0, len(order))
	for _, ep := range order {
		out = append(out, *byEndpoint[ep])
	}
	return out, nil
}
