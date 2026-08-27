package opfor

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestSleepRegexWholeNoCaptureFastPathPreservesIndices(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		pattern string
		input   string
		want    []int
	}{
		{name: "ascii_match", pattern: `[a-z]+[0-9]+`, input: "operator42", want: []int{0, 10}},
		{name: "ascii_miss", pattern: `[a-z]+[0-9]+`, input: "operator", want: nil},
		{name: "supplementary_match", pattern: `.+`, input: "a😀b", want: []int{0, 6}},
		{name: "empty_match", pattern: `.*`, input: "", want: []int{0, 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expression, err := compileSleepRegex(test.pattern, true)
			if err != nil {
				t.Fatal(err)
			}
			got, err := expression.FindStringSubmatchIndexContext(context.Background(), test.input)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("indices = %v, want %v", got, test.want)
			}
			if captures := sleepRegexCaptures(test.input, got); len(captures) != 0 {
				t.Fatalf("captures = %v, want none", captures)
			}
		})
	}
}

func TestSleepRegexWholeNoCaptureFastPathCancellation(t *testing.T) {
	t.Parallel()
	expression, err := compileSleepRegex(`(?>a+)+b`, true)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	match, err := expression.FindStringSubmatchIndexContext(ctx, "aaaaaaaaaaaaaaaa")
	if match != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("match = (%v, %v), want (nil, context.Canceled)", match, err)
	}
}
