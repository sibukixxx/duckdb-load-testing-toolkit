// Command duckload is a small, offline CLI for the load-testing toolkit's
// analysis layer. It opens a `.duckdb` result file directly (no sidecar
// process required) and runs the same analysis code the HTTP API uses, so
// a shared result file can be inspected without standing up a server.
//
// Commands are deliberately few and named after what this project's users
// actually do with a result file: see a summary, look at endpoints,
// compare two runs, or find out why a run regressed.
package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/fixtures"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/queries"
	"github.com/sibukixxx/duckdb-load-testing-toolkit/sidecar-go/analysis/validator"
)

// parseArgs splits args into flags (--name value) and positional arguments
// regardless of their relative order, then runs them through fs.Parse. The
// stdlib flag package alone stops parsing at the first non-flag argument,
// which would force a fixed `<file> --flag value` or `--flag value <file>`
// order; this lets `duckload summary result.duckdb --run-id x` and
// `duckload summary --run-id x result.duckdb` both work.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var flagArgs, positional []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flagArgs = append(flagArgs, a)
			if !strings.Contains(a, "=") && i+1 < len(args) {
				i++
				flagArgs = append(flagArgs, args[i])
			}
			continue
		}
		positional = append(positional, a)
	}
	if err := fs.Parse(flagArgs); err != nil {
		return nil, err
	}
	return positional, nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "summary":
		err = runSummary(os.Args[2:])
	case "endpoints":
		err = runEndpoints(os.Args[2:])
	case "compare":
		err = runCompare(os.Args[2:])
	case "diagnose":
		err = runDiagnose(os.Args[2:])
	case "check-analysis":
		err = runCheckAnalysis(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "duckload: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "duckload: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `duckload — offline analysis for duckdb-load-testing-toolkit result files

Usage:
  duckload summary <result.duckdb> --run-id <id>
  duckload endpoints <result.duckdb> --run-id <id>
  duckload compare <result.duckdb> --baseline <id> --current <id>
  duckload diagnose <result.duckdb> --baseline <id> --current <id>
  duckload check-analysis
`)
}

func openReadOnly(path string) (*sql.DB, error) {
	if path == "" {
		return nil, fmt.Errorf("a .duckdb file path is required")
	}
	db, err := sql.Open("duckdb", path+"?access_mode=READ_ONLY")
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", path, err)
	}
	return db, nil
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func runSummary(args []string) error {
	fs := flag.NewFlagSet("summary", flag.ExitOnError)
	runID := fs.String("run-id", "", "run_id to summarize")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) < 1 || *runID == "" {
		return fmt.Errorf("usage: duckload summary <result.duckdb> --run-id <id>")
	}

	db, err := openReadOnly(positional[0])
	if err != nil {
		return err
	}
	defer db.Close()

	comparator := analysis.NewComparator(db, analysis.DefaultThresholds())
	stats, err := comparator.GetRunStats(*runID)
	if err != nil {
		return fmt.Errorf("failed to summarize run %q: %w", *runID, err)
	}
	return printJSON(stats)
}

func runEndpoints(args []string) error {
	fs := flag.NewFlagSet("endpoints", flag.ExitOnError)
	runID := fs.String("run-id", "", "run_id to analyze")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) < 1 || *runID == "" {
		return fmt.Errorf("usage: duckload endpoints <result.duckdb> --run-id <id>")
	}

	db, err := openReadOnly(positional[0])
	if err != nil {
		return err
	}
	defer db.Close()

	stats, err := analysis.NewEndpointAnalyzer(db).GetEndpointStats(*runID)
	if err != nil {
		return fmt.Errorf("failed to analyze endpoints for run %q: %w", *runID, err)
	}
	return printJSON(stats)
}

func runCompare(args []string) error {
	fs := flag.NewFlagSet("compare", flag.ExitOnError)
	baseline := fs.String("baseline", "", "baseline run_id")
	current := fs.String("current", "", "current run_id")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) < 1 || *baseline == "" || *current == "" {
		return fmt.Errorf("usage: duckload compare <result.duckdb> --baseline <id> --current <id>")
	}

	db, err := openReadOnly(positional[0])
	if err != nil {
		return err
	}
	defer db.Close()

	result, err := analysis.NewComparator(db, analysis.DefaultThresholds()).Compare(*baseline, *current)
	if err != nil {
		return fmt.Errorf("failed to compare %q vs %q: %w", *baseline, *current, err)
	}
	return printJSON(result)
}

func runDiagnose(args []string) error {
	fs := flag.NewFlagSet("diagnose", flag.ExitOnError)
	baseline := fs.String("baseline", "", "baseline run_id")
	current := fs.String("current", "", "current run_id")
	positional, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if len(positional) < 1 || *baseline == "" || *current == "" {
		return fmt.Errorf("usage: duckload diagnose <result.duckdb> --baseline <id> --current <id>")
	}

	db, err := openReadOnly(positional[0])
	if err != nil {
		return err
	}
	defer db.Close()

	thresholds := analysis.DefaultThresholds()
	analyzer := analysis.NewEndpointAnalyzer(db)

	regressions, err := analysis.DetectEndpointRegressions(analyzer, *baseline, *current, thresholds)
	if err != nil {
		return fmt.Errorf("failed to detect regressions: %w", err)
	}
	explanations, err := analysis.Explain(db, *baseline, *current, thresholds)
	if err != nil {
		return fmt.Errorf("failed to build explanation: %w", err)
	}

	return printJSON(struct {
		Regressions  []analysis.RegressionFinding `json:"regressions"`
		Explanations []analysis.Explanation       `json:"explanations"`
	}{Regressions: regressions, Explanations: explanations})
}

// runCheckAnalysis validates every catalog query against every synthetic
// fixture — the same check CI runs — so a contributor can run it locally
// before pushing a change to an analysis query.
func runCheckAnalysis(args []string) error {
	fs := flag.NewFlagSet("check-analysis", flag.ExitOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	failed := false
	for _, scenario := range fixtures.All() {
		db, cleanup, err := fixtures.BuildDB(scenario)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL %-20s build fixture: %v\n", scenario, err)
			failed = true
			continue
		}

		_, err = validator.ValidateAll(db, func(q queries.Query) []any {
			return []any{fixtures.CurrentRunID}
		})
		cleanup()
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL %-20s %v\n", scenario, err)
			failed = true
			continue
		}
		fmt.Printf("PASS %-20s all catalog queries validated\n", scenario)
	}

	if failed {
		return fmt.Errorf("one or more analysis queries failed validation")
	}
	return nil
}
