package conformance

import (
	"context"
	"testing"
)

// Require runs SuiteVersion and reports every failed case through t. It is
// intended for an importing project's _test.go files.
func Require(t testing.TB, ctx context.Context, factory Factory) Report {
	t.Helper()
	report := Run(ctx, factory)
	for _, result := range report.Results {
		if result.Err != nil {
			t.Errorf("OPFOR importer conformance %s: %v", result.Name, result.Err)
		}
	}
	return report
}
