package fixtures

import (
	"reflect"
	"testing"
)

func TestGenerateGate_Deterministic(t *testing.T) {
	for _, sc := range AllGateScenarios() {
		a := GenerateGate(sc)
		b := GenerateGate(sc)
		if len(a) == 0 {
			t.Fatalf("%s: generated no events", sc)
		}
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("%s: non-deterministic output between calls", sc)
		}
	}
}

func TestBuildGateDB(t *testing.T) {
	for _, sc := range AllGateScenarios() {
		db, cleanup, err := BuildGateDB(sc)
		if err != nil {
			t.Fatalf("%s: BuildGateDB failed: %v", sc, err)
		}
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM metrics").Scan(&count); err != nil {
			cleanup()
			t.Fatalf("%s: query failed: %v", sc, err)
		}
		if count != len(GenerateGate(sc)) {
			t.Errorf("%s: row count = %d, expected %d", sc, count, len(GenerateGate(sc)))
		}
		cleanup()
	}
}
