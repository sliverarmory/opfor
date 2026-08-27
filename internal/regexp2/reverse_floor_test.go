package regexp2

import (
	"context"
	"testing"
)

func TestFindRunesMatchStartingAtWithReverseFloor(t *testing.T) {
	t.Parallel()
	input := []rune{'a', 0xd83d, 0xde00}
	for _, test := range []struct {
		pattern string
		want    bool
	}{
		{pattern: `(?=.)`, want: true},
		{pattern: `(?<!.)`, want: true},
		{pattern: `(?<=.)`, want: false},
		{pattern: `^`, want: false},
	} {
		expression, err := Compile(test.pattern, RE2)
		if err != nil {
			t.Fatalf("compile %q: %v", test.pattern, err)
		}
		match, err := expression.FindRunesMatchStartingAtWithReverseFloorContext(
			context.Background(), input, 2, 2,
		)
		if err != nil {
			t.Fatalf("match %q: %v", test.pattern, err)
		}
		got := match != nil && match.Index == 2 && match.Length == 0
		if got != test.want {
			t.Errorf("match %q at floor = %v, want %v (match=%v)", test.pattern, got, test.want, match)
		}
	}
}
