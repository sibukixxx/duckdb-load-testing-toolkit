package storage

import (
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/example/duckdb-sidecar/models"
)

// DuckDBStorage handles DuckDB operations for metrics storage
type DuckDBStorage struct {
	db       *sql.DB
	dbFile   string
	buffer   []models.Event
	bufferMu sync.Mutex
}

// NewDuckDBStorage creates a new DuckDB storage instance
func NewDuckDBStorage(dbFile string) (*DuckDBStorage, error) {
	db, err := sql.Open("duckdb", dbFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open duckdb: %w", err)
	}

	storage := &DuckDBStorage{
		db:     db,
		dbFile: dbFile,
		buffer: make([]models.Event, 0),
	}

	if err := storage.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	return storage, nil
}

// initSchema creates the metrics table with extended schema
func (s *DuckDBStorage) initSchema() error {
	schema := `
	CREATE TABLE IF NOT EXISTS metrics (
		-- Identification
		ts BIGINT,
		run_id VARCHAR,
		pod_id VARCHAR,
		vu INTEGER,
		iter INTEGER,

		-- Request info
		method VARCHAR,
		url VARCHAR,
		name VARCHAR,

		-- Response info
		status INTEGER,
		body_len INTEGER,

		-- Detailed timings (milliseconds)
		rtt DOUBLE,
		dns_lookup DOUBLE,
		tcp_connect DOUBLE,
		tls_handshake DOUBLE,
		ttfb DOUBLE,
		content_transfer DOUBLE,

		-- Size metrics
		request_size INTEGER,
		response_size INTEGER,

		-- Error handling
		error_code VARCHAR,
		error_msg VARCHAR,

		-- Custom tags (JSON)
		tags VARCHAR
	);`

	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}
	return nil
}

// AddEvent adds an event to the buffer
func (s *DuckDBStorage) AddEvent(event models.Event) {
	s.bufferMu.Lock()
	defer s.bufferMu.Unlock()
	s.buffer = append(s.buffer, event)
}

// AddEvents adds multiple events to the buffer
func (s *DuckDBStorage) AddEvents(events []models.Event) {
	s.bufferMu.Lock()
	defer s.bufferMu.Unlock()
	s.buffer = append(s.buffer, events...)
}

// BufferSize returns the current buffer size
func (s *DuckDBStorage) BufferSize() int {
	s.bufferMu.Lock()
	defer s.bufferMu.Unlock()
	return len(s.buffer)
}

// Flush writes buffered events to DuckDB
func (s *DuckDBStorage) Flush() (int, error) {
	s.bufferMu.Lock()
	if len(s.buffer) == 0 {
		s.bufferMu.Unlock()
		return 0, nil
	}
	batch := s.buffer
	s.buffer = make([]models.Event, 0)
	s.bufferMu.Unlock()

	count, err := s.writeBatch(batch)
	if err != nil {
		// Put events back in buffer on failure
		s.bufferMu.Lock()
		s.buffer = append(batch, s.buffer...)
		s.bufferMu.Unlock()
		return 0, err
	}

	return count, nil
}

// writeBatch writes a batch of events to DuckDB via CSV
func (s *DuckDBStorage) writeBatch(batch []models.Event) (int, error) {
	tmp := s.dbFile + ".tmp.csv"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, fmt.Errorf("create tmp csv error: %w", err)
	}
	defer os.Remove(tmp)

	w := csv.NewWriter(f)

	// Header
	header := []string{
		"ts", "run_id", "pod_id", "vu", "iter",
		"method", "url", "name",
		"status", "body_len",
		"rtt", "dns_lookup", "tcp_connect", "tls_handshake", "ttfb", "content_transfer",
		"request_size", "response_size",
		"error_code", "error_msg",
		"tags",
	}
	if err := w.Write(header); err != nil {
		f.Close()
		return 0, fmt.Errorf("csv header write error: %w", err)
	}

	for _, e := range batch {
		tagsJSON := ""
		if e.Tags != nil {
			if data, err := json.Marshal(e.Tags); err == nil {
				tagsJSON = string(data)
			}
		}

		row := []string{
			strconv.FormatInt(e.Ts, 10),
			e.RunID,
			e.PodID,
			strconv.Itoa(e.VU),
			strconv.Itoa(e.Iter),
			e.Method,
			e.URL,
			e.Name,
			strconv.Itoa(e.Status),
			strconv.Itoa(e.BodyLen),
			fmt.Sprintf("%f", e.RTT),
			fmt.Sprintf("%f", e.DNSLookup),
			fmt.Sprintf("%f", e.TCPConnect),
			fmt.Sprintf("%f", e.TLSHandshake),
			fmt.Sprintf("%f", e.TTFB),
			fmt.Sprintf("%f", e.ContentTransfer),
			strconv.Itoa(e.RequestSize),
			strconv.Itoa(e.ResponseSize),
			e.ErrorCode,
			e.ErrorMsg,
			tagsJSON,
		}
		if err := w.Write(row); err != nil {
			f.Close()
			return 0, fmt.Errorf("csv write error: %w", err)
		}
	}

	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return 0, fmt.Errorf("csv flush error: %w", err)
	}
	f.Close()

	// COPY into DuckDB
	q := fmt.Sprintf("COPY metrics FROM '%s' (HEADER true, AUTO_DETECT true);", tmp)
	if _, err := s.db.Exec(q); err != nil {
		return 0, fmt.Errorf("duckdb copy error: %w", err)
	}

	return len(batch), nil
}

// GetDBFile returns the database file path
func (s *DuckDBStorage) GetDBFile() string {
	return s.dbFile
}

// Close closes the database connection
func (s *DuckDBStorage) Close() error {
	return s.db.Close()
}

// GetDB returns the underlying database connection
func (s *DuckDBStorage) GetDB() *sql.DB {
	return s.db
}

// Query executes a query and returns the result
func (s *DuckDBStorage) Query(query string) (*sql.Rows, error) {
	return s.db.Query(query)
}

// GetStats returns basic statistics from the metrics table
func (s *DuckDBStorage) GetStats() (*MetricsStats, error) {
	query := `
	SELECT
		COUNT(*) as total_requests,
		COUNT(CASE WHEN status >= 400 OR error_code != '' THEN 1 END) as error_count,
		AVG(rtt) as avg_rtt,
		PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY rtt) as p50_rtt,
		PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY rtt) as p95_rtt,
		PERCENTILE_CONT(0.99) WITHIN GROUP (ORDER BY rtt) as p99_rtt,
		MIN(ts) as start_ts,
		MAX(ts) as end_ts
	FROM metrics
	`
	row := s.db.QueryRow(query)

	var stats MetricsStats
	err := row.Scan(
		&stats.TotalRequests,
		&stats.ErrorCount,
		&stats.AvgRTT,
		&stats.P50RTT,
		&stats.P95RTT,
		&stats.P99RTT,
		&stats.StartTs,
		&stats.EndTs,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	return &stats, nil
}

// MetricsStats holds aggregate statistics
type MetricsStats struct {
	TotalRequests int64
	ErrorCount    int64
	AvgRTT        float64
	P50RTT        float64
	P95RTT        float64
	P99RTT        float64
	StartTs       int64
	EndTs         int64
}

// ErrorRate returns the error rate as a percentage
func (s *MetricsStats) ErrorRate() float64 {
	if s.TotalRequests == 0 {
		return 0
	}
	return float64(s.ErrorCount) / float64(s.TotalRequests) * 100
}

// DurationSeconds returns the test duration in seconds
func (s *MetricsStats) DurationSeconds() float64 {
	return float64(s.EndTs-s.StartTs) / 1000.0
}

// RPS returns requests per second
func (s *MetricsStats) RPS() float64 {
	duration := s.DurationSeconds()
	if duration == 0 {
		return 0
	}
	return float64(s.TotalRequests) / duration
}
