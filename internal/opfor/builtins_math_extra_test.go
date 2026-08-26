package opfor

import (
	"context"
	"math"
	"reflect"
	"sort"
	"testing"
)

func TestMathExtraFunctionSet(t *testing.T) {
	functions := (&Runtime{}).mathExtraFunctions()
	got := make([]string, 0, len(functions))
	for name := range functions {
		got = append(got, name)
	}
	sort.Strings(got)
	want := []string{
		"acos", "asin", "atan", "atan2", "degrees", "exp", "formatNumber",
		"not", "parseNumber", "radians", "uint",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("function names = %q, want %q", got, want)
	}
}

func TestMathExtraFunctionsMatchBasicNumbers(t *testing.T) {
	functions := (&Runtime{}).mathExtraFunctions()
	tests := []struct {
		name string
		args []Value
		want float64
	}{
		{name: "acos", args: []Value{Double(0)}, want: math.Pi / 2},
		{name: "asin", args: []Value{Double(1)}, want: math.Pi / 2},
		{name: "atan", args: []Value{Double(1)}, want: math.Pi / 4},
		{name: "atan2", args: []Value{Double(1), Double(1)}, want: math.Pi / 4},
		{name: "radians", args: []Value{Double(180)}, want: math.Pi},
		{name: "degrees", args: []Value{Double(math.Pi)}, want: 180},
		{name: "exp", args: []Value{Double(1)}, want: math.E},
	}
	for _, test := range tests {
		got := callMathExtra(t, functions, test.name, test.args...)
		if got.Kind() != KindDouble || math.Abs(got.Float64()-test.want) > 1e-12 {
			t.Fatalf("%s(%v) = (%s, %.17g), want double %.17g", test.name, test.args, got.Kind(), got.Float64(), test.want)
		}
	}
	if got := callMathExtra(t, functions, "acos"); got.Kind() != KindDouble || got.Float64() != math.Pi/2 {
		t.Fatalf("acos() = %s, want pi/2", got.Describe())
	}
}

func TestMathExtraIntegerConversions(t *testing.T) {
	functions := (&Runtime{}).mathExtraFunctions()
	if got := callMathExtra(t, functions, "not", Int(15)); got.Kind() != KindInt || got.Int32() != -16 {
		t.Fatalf("not(int 15) = (%s, %s), want int -16", got.Kind(), got.Describe())
	}
	if got := callMathExtra(t, functions, "not", Long(15)); got.Kind() != KindLong || got.Int64() != -16 {
		t.Fatalf("not(long 15) = (%s, %s), want long -16", got.Kind(), got.Describe())
	}
	if got := callMathExtra(t, functions, "not", String("15")); got.Kind() != KindLong || got.Int64() != -16 {
		t.Fatalf("not(string 15) = (%s, %s), want long -16", got.Kind(), got.Describe())
	}
	if got := callMathExtra(t, functions, "uint", Int(-1)); got.Kind() != KindLong || got.Int64() != 4_294_967_295 {
		t.Fatalf("uint(-1) = (%s, %s), want long 4294967295", got.Kind(), got.Describe())
	}
	if got := callMathExtra(t, functions, "uint"); got.Kind() != KindLong || got.Int64() != 0 {
		t.Fatalf("uint() = (%s, %s), want long 0", got.Kind(), got.Describe())
	}
	if _, err := functions["not"](context.Background(), mathExtraInvocation("not")); err == nil {
		t.Fatal("not() unexpectedly succeeded")
	}
}

func TestParseAndFormatNumberMatchBigInteger(t *testing.T) {
	functions := (&Runtime{}).mathExtraFunctions()
	parseTests := []struct {
		args []Value
		want int64
	}{
		{want: 0},
		{args: []Value{String("ff"), Int(16)}, want: 255},
		{args: []Value{String("-101"), Int(2)}, want: -5},
		{args: []Value{String("18446744073709551615")}, want: -1},
		{args: []Value{String("18446744073709551616")}, want: 0},
		{args: []Value{String("-9223372036854775809")}, want: math.MaxInt64},
		{args: []Value{String("١٢٣")}, want: 123},
		{args: []Value{String("-۰۰۱۲")}, want: -12},
		{args: []Value{String("ＦＦ"), Int(16)}, want: 255},
	}
	for _, test := range parseTests {
		got := callMathExtra(t, functions, "parseNumber", test.args...)
		if got.Kind() != KindLong || got.Int64() != test.want {
			t.Fatalf("parseNumber(%v) = (%s, %s), want long %d", test.args, got.Kind(), got.Describe(), test.want)
		}
	}

	formatTests := []struct {
		args []Value
		want string
	}{
		{want: "0"},
		{args: []Value{String("255"), Int(16)}, want: "ff"},
		{args: []Value{String("ff"), Int(16), Int(2)}, want: "11111111"},
		{args: []Value{String("-255"), Int(16)}, want: "-ff"},
		{args: []Value{String("123456789012345678901234567890"), Int(36)}, want: "byw97um9s91dlz68tsi"},
		{args: []Value{String("15"), Int(37)}, want: "15"},
		{args: []Value{String("15"), Int(1)}, want: "15"},
		{args: []Value{String("ff"), Int(16), Int(37)}, want: "255"},
		{args: []Value{String("15"), Int(10), Int(2), Int(8)}, want: "15"},
		{args: []Value{String("١٢٣"), Int(10), Int(16)}, want: "7b"},
		{args: []Value{String("ＦＦ"), Int(16), Int(10)}, want: "255"},
	}
	for _, test := range formatTests {
		got := callMathExtra(t, functions, "formatNumber", test.args...)
		if got.Kind() != KindString || got.String() != test.want {
			t.Fatalf("formatNumber(%v) = (%s, %s), want string %q", test.args, got.Kind(), got.Describe(), test.want)
		}
	}

	for _, test := range []struct {
		invocation Invocation
		want       string
	}{
		{mathExtraInvocation("parseNumber", String("xyz"), Int(10)), `For input string: "xyz"`},
		{mathExtraInvocation("parseNumber", String("2"), Int(2)), `For input string: "2" under radix 2`},
		{mathExtraInvocation("parseNumber", String("-2"), Int(2)), `For input string: "2" under radix 2`},
		{mathExtraInvocation("parseNumber", String("٢"), Int(2)), `For input string: "٢" under radix 2`},
		{mathExtraInvocation("parseNumber", String("1-0"), Int(10)), "Illegal embedded sign character"},
		{mathExtraInvocation("parseNumber", String("+1+"), Int(10)), "Illegal embedded sign character"},
		{mathExtraInvocation("parseNumber", String("123456789x"), Int(10)), `For input string: "23456789x"`},
		{mathExtraInvocation("parseNumber", String("1"), Int(1)), "Radix out of range"},
		{mathExtraInvocation("parseNumber", String(""), Int(10)), "Zero length BigInteger"},
		{mathExtraInvocation("formatNumber", String("1"), Int(1), Int(10)), "Radix out of range"},
		{mathExtraInvocation("formatNumber", String("ff"), Int(16), Int(2), Int(8)), `For input string: "ff"`},
	} {
		_, err := functions[test.invocation.Name](context.Background(), test.invocation)
		if err == nil || err.Error() != test.want {
			t.Fatalf("%s invalid input error = %v, want %q", test.invocation.Name, err, test.want)
		}
	}
}

func mathExtraInvocation(name string, values ...Value) Invocation {
	arguments := make([]Argument, len(values))
	for index, value := range values {
		arguments[index] = Argument{Value: value}
	}
	return Invocation{Name: name, Arguments: arguments}
}

func callMathExtra(t *testing.T, functions map[string]NativeFunc, name string, values ...Value) Value {
	t.Helper()
	function := functions[name]
	if function == nil {
		t.Fatalf("function %q is not registered", name)
	}
	value, err := function(context.Background(), mathExtraInvocation(name, values...))
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return value
}
