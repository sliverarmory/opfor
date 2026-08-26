package regexp2

import (
	"context"
	"errors"
	"testing"
)

func TestFindRunesMatchContextCancellation(t *testing.T) {
	expression := MustCompile(`^(a+)+$`, None)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	match, err := expression.FindRunesMatchContext(ctx, []rune("aaaaaaaaaaaaaaaa!"))
	if match != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("FindRunesMatchContext = (%v, %v), want (nil, context.Canceled)", match, err)
	}
}

func TestFindNextMatchContextCancellation(t *testing.T) {
	expression := MustCompile(`a`, None)
	first, err := expression.FindRunesMatch([]rune("aa"))
	if err != nil || first == nil {
		t.Fatalf("FindRunesMatch = (%v, %v), want a match", first, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	next, err := expression.FindNextMatchContext(ctx, first)
	if next != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("FindNextMatchContext = (%v, %v), want (nil, context.Canceled)", next, err)
	}
}
