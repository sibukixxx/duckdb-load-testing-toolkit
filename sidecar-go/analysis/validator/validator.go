// Package validator provides a lightweight validation layer for catalog
// SQL queries (see analysis/queries). It leans on DuckDB itself — PRAGMA
// introspection, PREPARE/bind, and EXPLAIN — rather than a hand-rolled SQL
// linter, so it only needs to check what DuckDB cannot already catch by
// running the query: required columns, required parameters, and the
// resulting result-set shape.
package validator

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/queries"
)

// Result carries everything a caller (a test, `duckload check-analysis`, or
// CI) needs to report on one query's validation.
type Result struct {
	QueryID       string
	Columns       []string // actual result-set columns from executing the query
	MissingOutput []string // ExpectedOutput columns absent from Columns, if any
}

// Validate checks that q's declared requirements hold against db and that
// the query actually runs, in four steps:
//  1. required_columns exist on the `metrics` table;
//  2. the number of bound params matches q.Parameters;
//  3. EXPLAIN succeeds (catches parse/bind errors without scanning rows);
//  4. the query executes and its result columns are compared against
//     ExpectedOutput.
func Validate(db *sql.DB, q queries.Query, params ...any) (*Result, error) {
	if err := checkRequiredColumns(db, q); err != nil {
		return nil, err
	}

	if len(params) != len(q.Parameters) {
		return nil, fmt.Errorf(
			"validator: %s expects %d parameter(s) (%v), got %d",
			q.ID, len(q.Parameters), paramNames(q.Parameters), len(params),
		)
	}

	if _, err := db.Query("EXPLAIN "+q.SQL, params...); err != nil {
		return nil, fmt.Errorf("validator: %s failed EXPLAIN: %w", q.ID, err)
	}

	rows, err := db.Query(q.SQL, params...)
	if err != nil {
		return nil, fmt.Errorf("validator: %s failed to execute: %w", q.ID, err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("validator: %s failed to read columns: %w", q.ID, err)
	}

	res := &Result{QueryID: q.ID, Columns: cols}
	if len(q.ExpectedOutput) > 0 {
		res.MissingOutput = missing(q.ExpectedOutput, cols)
	}

	return res, nil
}

// checkRequiredColumns confirms every column q declares as required is
// present on the `metrics` table, using DuckDB's own catalog rather than
// parsing the SQL to figure out which columns it touches.
func checkRequiredColumns(db *sql.DB, q queries.Query) error {
	if len(q.RequiredColumns) == 0 {
		return nil
	}

	rows, err := db.Query(`
		SELECT column_name FROM information_schema.columns
		WHERE table_name = 'metrics'
	`)
	if err != nil {
		return fmt.Errorf("validator: failed to introspect metrics schema: %w", err)
	}
	defer rows.Close()

	present := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("validator: failed to scan column name: %w", err)
		}
		present[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}

	var missingCols []string
	for _, c := range q.RequiredColumns {
		if !present[c] {
			missingCols = append(missingCols, c)
		}
	}
	if len(missingCols) > 0 {
		return fmt.Errorf(
			"validator: %s requires column(s) not present on metrics: %s",
			q.ID, strings.Join(missingCols, ", "),
		)
	}
	return nil
}

func missing(expected, actual []string) []string {
	have := map[string]bool{}
	for _, c := range actual {
		have[c] = true
	}
	var out []string
	for _, c := range expected {
		if !have[c] {
			out = append(out, c)
		}
	}
	return out
}

func paramNames(params []queries.Parameter) []string {
	out := make([]string, len(params))
	for i, p := range params {
		out[i] = p.Name
	}
	return out
}

// ValidateAll runs Validate for every catalog query against db, using
// paramsFor to supply that query's bind arguments (e.g. a fixture's
// run_id). It returns one Result per query that passed and an error on the
// first failure, so CI fails fast on a broken analysis query.
func ValidateAll(db *sql.DB, paramsFor func(q queries.Query) []any) ([]*Result, error) {
	var results []*Result
	for _, q := range queries.All() {
		res, err := Validate(db, q, paramsFor(q)...)
		if err != nil {
			return results, err
		}
		results = append(results, res)
	}
	return results, nil
}
