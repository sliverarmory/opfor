package source_test

import (
	"reflect"
	"testing"

	"github.com/sliverarmory/opfor"
	"github.com/sliverarmory/opfor/source"
)

func TestRootAliasesRetainPublicSourceTypeIdentities(t *testing.T) {
	rootSource := opfor.NewSource("identity.sl", []byte("println('ok');"))
	var publicSource source.Source = rootSource
	var roundTrip opfor.Source = publicSource
	if roundTrip.Name != "identity.sl" {
		t.Fatalf("round-trip source name = %q", roundTrip.Name)
	}

	tests := []struct {
		name  string
		value any
	}{
		{name: "Source", value: opfor.Source{}},
		{name: "Position", value: opfor.Position{}},
		{name: "Span", value: opfor.Span{}},
		{name: "Severity", value: opfor.SeverityError},
		{name: "Diagnostic", value: opfor.Diagnostic{}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, want := reflect.TypeOf(test.value).PkgPath(), "github.com/sliverarmory/opfor/source"; got != want {
				t.Fatalf("type package = %q, want %q", got, want)
			}
		})
	}
}

func TestDiagnosticFormatting(t *testing.T) {
	diagnostic := source.Diagnostic{
		Severity: source.SeverityError,
		Code:     "PAR001",
		Message:  "Syntax error",
		Span: source.Span{
			Source: "fixture.sl",
			Start:  source.Position{Line: 2, Column: 3},
			End:    source.Position{Line: 2, Column: 7},
		},
	}
	if got, want := diagnostic.Error(), "fixture.sl:2:3-7: error PAR001: Syntax error"; got != want {
		t.Fatalf("Diagnostic.Error() = %q, want %q", got, want)
	}
}
