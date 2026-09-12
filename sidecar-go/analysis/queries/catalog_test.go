package queries

import "testing"

func TestAll_HasKnownQueries(t *testing.T) {
	ids := map[string]bool{}
	for _, q := range All() {
		ids[q.ID] = true

		if q.Purpose == "" {
			t.Errorf("query %s: missing purpose", q.ID)
		}
		if len(q.RequiredColumns) == 0 {
			t.Errorf("query %s: missing required_columns", q.ID)
		}
		if q.SQL == "" {
			t.Errorf("query %s: empty SQL body", q.ID)
		}
	}

	for _, want := range []string{EndpointSummary, EndpointStatusDistribution, PodEndpointSummary} {
		if !ids[want] {
			t.Errorf("expected catalog to contain %q", want)
		}
	}
}

func TestGet(t *testing.T) {
	q, ok := Get(EndpointSummary)
	if !ok {
		t.Fatalf("expected %s to be found", EndpointSummary)
	}
	if len(q.Parameters) != 1 || q.Parameters[0].Name != "run_id" {
		t.Errorf("unexpected parameters: %+v", q.Parameters)
	}
	if len(q.ExpectedOutput) == 0 {
		t.Error("expected non-empty ExpectedOutput")
	}

	if _, ok := Get("does-not-exist"); ok {
		t.Error("expected Get for unknown id to return ok=false")
	}
}

func TestMustGet_PanicsOnUnknown(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected MustGet to panic for unknown id")
		}
	}()
	MustGet("does-not-exist")
}
