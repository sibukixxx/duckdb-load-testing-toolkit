package validator

import (
	"testing"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/fixtures"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/queries"
)

// TestValidateAll_AcrossAllFixtures is the CI-facing check described by the
// project's analysis validation requirement: every catalog query must
// parse, bind, EXPLAIN, execute, and return its declared output columns
// against every synthetic fixture.
func TestValidateAll_AcrossAllFixtures(t *testing.T) {
	for _, scenario := range fixtures.All() {
		scenario := scenario
		t.Run(string(scenario), func(t *testing.T) {
			db, cleanup, err := fixtures.BuildDB(scenario)
			if err != nil {
				t.Fatalf("BuildDB failed: %v", err)
			}
			defer cleanup()

			results, err := ValidateAll(db, func(q queries.Query) []any {
				return []any{fixtures.CurrentRunID}
			})
			if err != nil {
				t.Fatalf("ValidateAll failed: %v", err)
			}

			for _, r := range results {
				if len(r.MissingOutput) > 0 {
					t.Errorf("%s: missing expected output columns: %v (got %v)", r.QueryID, r.MissingOutput, r.Columns)
				}
			}
			if len(results) != len(queries.All()) {
				t.Errorf("expected %d results, got %d", len(queries.All()), len(results))
			}
		})
	}
}

func TestValidate_MissingRequiredColumn(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.Healthy)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	bad := queries.Query{
		ID:              "bad_query",
		RequiredColumns: []string{"this_column_does_not_exist"},
		Parameters:      []queries.Parameter{{Name: "run_id", Type: "VARCHAR"}},
		SQL:             "SELECT run_id FROM metrics WHERE run_id = ?",
	}

	if _, err := Validate(db, bad, fixtures.CurrentRunID); err == nil {
		t.Fatal("expected Validate to fail for a query requiring a missing column")
	}
}

func TestValidate_WrongParameterCount(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.Healthy)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	q := queries.MustGet(queries.EndpointSummary)
	if _, err := Validate(db, q); err == nil {
		t.Fatal("expected Validate to fail when no parameters are bound")
	}
}

func TestValidate_BrokenSQLFailsExplain(t *testing.T) {
	db, cleanup, err := fixtures.BuildDB(fixtures.Healthy)
	if err != nil {
		t.Fatalf("BuildDB failed: %v", err)
	}
	defer cleanup()

	broken := queries.Query{
		ID:         "broken_query",
		Parameters: []queries.Parameter{{Name: "run_id", Type: "VARCHAR"}},
		SQL:        "SELECT this_is_not_a_column FROM metrics WHERE run_id = ?",
	}

	if _, err := Validate(db, broken, fixtures.CurrentRunID); err == nil {
		t.Fatal("expected Validate to fail EXPLAIN for a query referencing an unknown column")
	}
}
