// Package queries treats analysis SQL as a first-class, versioned artifact
// instead of a string embedded in handler code. Each query lives in its own
// .sql file under sql/ with a small metadata header (id, purpose, required
// columns, parameters, description, expected output columns) that the
// validator and analysis packages read without parsing SQL themselves.
package queries

import (
	"bufio"
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed sql/*.sql
var sqlFS embed.FS

// Parameter describes one positional "?" placeholder a query expects, in
// the order it must be bound.
type Parameter struct {
	Name string
	Type string
}

// Query is one catalog entry: a named, documented, purpose-built SQL
// statement for load-test analysis.
type Query struct {
	ID              string
	Purpose         string
	Description     string
	RequiredColumns []string
	Parameters      []Parameter
	ExpectedOutput  []string
	SQL             string
}

var catalog = map[string]Query{}

func init() {
	entries, err := sqlFS.ReadDir("sql")
	if err != nil {
		panic(fmt.Sprintf("queries: failed to read embedded sql dir: %v", err))
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		data, err := sqlFS.ReadFile("sql/" + e.Name())
		if err != nil {
			panic(fmt.Sprintf("queries: failed to read %s: %v", e.Name(), err))
		}
		q, err := parse(string(data))
		if err != nil {
			panic(fmt.Sprintf("queries: failed to parse %s: %v", e.Name(), err))
		}
		if q.ID == "" {
			panic(fmt.Sprintf("queries: %s is missing an '-- id:' header", e.Name()))
		}
		if _, exists := catalog[q.ID]; exists {
			panic(fmt.Sprintf("queries: duplicate query id %q", q.ID))
		}
		catalog[q.ID] = q
	}
}

// parse reads the leading "-- key: value" comment block as metadata and
// treats the remainder of the file as the SQL body.
func parse(content string) (Query, error) {
	var q Query
	var sqlLines []string
	var descLines []string
	inHeader := true

	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if inHeader && strings.HasPrefix(trimmed, "--") {
			body := strings.TrimSpace(strings.TrimPrefix(trimmed, "--"))
			if key, val, ok := splitHeaderLine(body); ok {
				switch key {
				case "id":
					q.ID = val
				case "purpose":
					q.Purpose = val
				case "description":
					descLines = append(descLines, val)
				case "required_columns":
					q.RequiredColumns = splitCSV(val)
				case "parameters":
					q.Parameters = parseParameters(val)
				case "expected_output":
					q.ExpectedOutput = splitCSV(val)
				}
				continue
			}
			// Continuation of a multi-line "description:" comment.
			if len(descLines) > 0 {
				descLines = append(descLines, strings.TrimPrefix(body, "  "))
				continue
			}
			continue
		}

		if trimmed == "" && inHeader {
			continue
		}
		inHeader = false
		sqlLines = append(sqlLines, line)
	}
	if err := scanner.Err(); err != nil {
		return Query{}, err
	}

	q.Description = strings.TrimSpace(strings.Join(descLines, " "))
	q.SQL = strings.TrimSpace(strings.Join(sqlLines, "\n"))
	return q, nil
}

func splitHeaderLine(s string) (key, val string, ok bool) {
	idx := strings.Index(s, ":")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(s[:idx])
	// Header keys are a single lowercase token; anything else (e.g. a
	// description sentence containing ':') is treated as prose, not a key.
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r == '_') {
			return "", "", false
		}
	}
	if key == "" {
		return "", "", false
	}
	val = strings.TrimSpace(s[idx+1:])
	return key, val, true
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func parseParameters(s string) []Parameter {
	names := splitCSV(s)
	params := make([]Parameter, 0, len(names))
	for _, n := range names {
		name, typ := n, "VARCHAR"
		if idx := strings.Index(n, ":"); idx >= 0 {
			name = strings.TrimSpace(n[:idx])
			typ = strings.TrimSpace(n[idx+1:])
		}
		params = append(params, Parameter{Name: name, Type: typ})
	}
	return params
}

// Get returns the query registered under id.
func Get(id string) (Query, bool) {
	q, ok := catalog[id]
	return q, ok
}

// MustGet returns the query registered under id, panicking if it does not
// exist. Intended for call sites where the id is a compile-time constant.
func MustGet(id string) Query {
	q, ok := catalog[id]
	if !ok {
		panic(fmt.Sprintf("queries: unknown query id %q", id))
	}
	return q
}

// All returns every catalog entry, sorted by ID, for validation and CLI
// introspection (e.g. `duckload check-analysis`).
func All() []Query {
	out := make([]Query, 0, len(catalog))
	for _, q := range catalog {
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Known query IDs, exposed as constants so callers get compile-time checked
// references instead of magic strings.
const (
	EndpointSummary            = "endpoint_summary"
	EndpointStatusDistribution = "endpoint_status_distribution"
	PodEndpointSummary         = "pod_endpoint_summary"
)
