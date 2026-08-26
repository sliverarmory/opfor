package opfor

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestAccessOrderedHashMovesReadsAndReplacements(t *testing.T) {
	t.Parallel()

	hash := NewAccessOrderedHash()
	hash.Set("a", String("apple"))
	hash.Set("b", String("bat"))
	hash.Set("c", String("cat"))
	if value, ok := hash.Get("a"); !ok || value.String() != "apple" {
		t.Fatalf("Get(a) = (%s, %v)", value.Describe(), ok)
	}
	if got, want := hash.Keys(), []string{"b", "c", "a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("keys after access = %q, want %q", got, want)
	}
	if err := hash.SetContext(context.Background(), "b", String("boy")); err != nil {
		t.Fatal(err)
	}
	if got, want := hash.Keys(), []string{"c", "a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("keys after replacement = %q, want %q", got, want)
	}
}

func TestOrderedHashMissAndRemovalPolicyArguments(t *testing.T) {
	t.Parallel()

	hash := NewOrderedHash()
	var missArguments, removalArguments []Value
	if err := hash.setMissPolicy(hashPolicyCallable(func(_ context.Context, values ...Value) (Value, error) {
		missArguments = append([]Value(nil), values...)
		return String(strings.ToUpper(values[1].String())), nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := hash.setRemovalPolicy(hashPolicyCallable(func(_ context.Context, values ...Value) (Value, error) {
		removalArguments = append([]Value(nil), values...)
		return Bool(hashActiveLen(values[0]) > 1), nil
	})); err != nil {
		t.Fatal(err)
	}

	first, err := hash.HashAt(context.Background(), "alpha")
	if err != nil || first.String() != "ALPHA" {
		t.Fatalf("HashAt(alpha) = (%s, %v)", first.Describe(), err)
	}
	second, err := hash.HashAt(context.Background(), "beta")
	if err != nil || second.String() != "BETA" {
		t.Fatalf("HashAt(beta) = (%s, %v)", second.Describe(), err)
	}
	if len(missArguments) != 2 || !missArguments[0].IdentityEqual(HashValue(hash)) || missArguments[1].String() != "beta" {
		t.Fatalf("miss arguments = %#v", missArguments)
	}
	if len(removalArguments) != 3 || !removalArguments[0].IdentityEqual(HashValue(hash)) ||
		removalArguments[1].String() != "alpha" || removalArguments[2].String() != "ALPHA" {
		t.Fatalf("removal arguments = %#v", removalArguments)
	}
	if _, exists := hash.Cell("alpha"); exists {
		t.Fatal("removal policy did not evict eldest entry")
	}
	if value, exists := hash.Get("beta"); !exists || value.String() != "BETA" {
		t.Fatalf("remaining beta = (%s, %v)", value.Describe(), exists)
	}
}

func TestOrderedHashPolicyErrorsPropagateWithoutDeadlock(t *testing.T) {
	t.Parallel()

	want := errors.New("miss failed")
	hash := NewOrderedHash()
	if err := hash.setMissPolicy(hashPolicyCallable(func(context.Context, ...Value) (Value, error) {
		return Null(), want
	})); err != nil {
		t.Fatal(err)
	}
	_, err := hash.HashAt(context.Background(), "missing")
	if !errors.Is(err, want) {
		t.Fatalf("HashAt error = %v, want %v", err, want)
	}
}

func TestOrderedHashPoliciesReceiveExactUTF16KeyValues(t *testing.T) {
	t.Parallel()

	high := sleepUTF16CharacterValue(0xd83d)
	hash := NewOrderedHash()
	var missKey, removalKey Value
	if err := hash.setMissPolicy(hashPolicyCallable(func(_ context.Context, values ...Value) (Value, error) {
		missKey = values[1]
		return String("high"), nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := hash.setRemovalPolicy(hashPolicyCallable(func(_ context.Context, values ...Value) (Value, error) {
		removalKey = values[1]
		return Bool(hashActiveLen(values[0]) > 1), nil
	})); err != nil {
		t.Fatal(err)
	}
	if value, err := hash.HashAtValue(context.Background(), high); err != nil || value.String() != "high" {
		t.Fatalf("surrogate miss = %s/%v", value.Describe(), err)
	}
	if got := sleepStringUnits(missKey); len(got) != 1 || got[0] != 0xd83d {
		t.Fatalf("miss-policy key units = %x, want d83d", got)
	}
	if err := hash.SetValueContext(context.Background(), BinaryString([]byte{0xc3, 0xa9}), String("binary")); err != nil {
		t.Fatal(err)
	}
	if got := sleepStringUnits(removalKey); len(got) != 1 || got[0] != 0xd83d {
		t.Fatalf("removal-policy key units = %x, want d83d", got)
	}
	if _, ok := hash.GetValue(high); ok {
		t.Fatal("removal policy did not evict the exact surrogate key")
	}
}

func hashActiveLen(value Value) int {
	hash, _ := value.Hash()
	if hash == nil {
		return 0
	}
	return len(activeHashKeys(hash, true))
}

type hashPolicyCallable func(context.Context, ...Value) (Value, error)

func (function hashPolicyCallable) Invoke(ctx context.Context, values ...Value) (Value, error) {
	return function(ctx, values...)
}
