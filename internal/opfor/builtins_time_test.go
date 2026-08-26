package opfor

import (
	"context"
	"testing"
	"time"
)

func TestTimeFunctionsUseConfiguredClock(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("test", -6*60*60)
	now := time.Date(2026, time.August, 23, 14, 5, 6, 123_000_000, location)
	runtime, err := New(WithClock(ClockFunc(func() time.Time { return now })))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	value, err := runtime.Eval(context.Background(), "time.sl", `
@result = @(ticks(), formatDate("yyyy-MM-dd HH:mm:ss.SSS"), formatDate(ticks(), "HH:mm:ss"));
return @result;
`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	array, _ := value.Array()
	values := array.Values()
	if values[0].Int64() != now.UnixMilli() || values[1].String() != "2026-08-23 14:05:06.123" || values[2].String() != "14:05:06" {
		t.Fatalf("time values = %s", value.Describe())
	}
}

func TestAggressorTimestampFunctionsUseConfiguredClockLocation(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("test-cst", -6*60*60)
	runtime, err := New(WithClock(ClockFunc(func() time.Time {
		return time.Date(2026, time.August, 24, 12, 0, 0, 0, location)
	})))
	if err != nil {
		t.Fatal(err)
	}
	instant := time.Date(2024, time.January, 2, 9, 4, 5, 987_000_000, time.UTC)

	dstamp, err := runtime.Invoke(context.Background(), "dstamp", Long(instant.UnixMilli()))
	if err != nil {
		t.Fatalf("dstamp: %v", err)
	}
	if got, want := dstamp.String(), "2024-01-02 03:04:05"; got != want {
		t.Fatalf("dstamp = %q, want %q", got, want)
	}

	tstamp, err := runtime.Invoke(context.Background(), "tstamp", Long(instant.UnixMilli()))
	if err != nil {
		t.Fatalf("tstamp: %v", err)
	}
	if got, want := tstamp.String(), "2024-01-02 03:04"; got != want {
		t.Fatalf("tstamp = %q, want %q", got, want)
	}
}

func TestAggressorTimestampFunctionsRequireMilliseconds(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dstamp", "tstamp"} {
		value, err := runtime.Invoke(context.Background(), name)
		if err == nil || !value.IsNull() {
			t.Fatalf("%s without milliseconds = (%s, %v), want null/error", name, value.Describe(), err)
		}
		want := "&" + name + ": expected at least 1 argument(s), received 0"
		if err.Error() != want {
			t.Fatalf("%s missing-argument error = %q, want %q", name, err, want)
		}
	}
}

func TestAggressorTimestampFunctionsAllowImporterOverrides(t *testing.T) {
	t.Parallel()

	override := func(_ context.Context, invocation Invocation) (Value, error) {
		return String("override:" + invocation.Name), nil
	}
	runtime, err := New(
		WithFunction("dstamp", override),
		WithFunction("tstamp", override),
	)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dstamp", "tstamp"} {
		value, err := runtime.Invoke(context.Background(), name, Long(0))
		if err != nil {
			t.Fatalf("%s override: %v", name, err)
		}
		if got, want := value.String(), "override:"+name; got != want {
			t.Fatalf("%s override = %q, want %q", name, got, want)
		}
	}
}

func TestParseDateUsesClockLocation(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("test", 2*60*60)
	runtime, err := New(WithClock(ClockFunc(func() time.Time {
		return time.Date(2026, 1, 1, 0, 0, 0, 0, location)
	})))
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.Invoke(context.Background(), "parseDate", String("yyyy-MM-dd HH:mm:ss"), String("2026-08-23 14:05:06"))
	if err != nil {
		t.Fatalf("parseDate: %v", err)
	}
	want := time.Date(2026, 8, 23, 14, 5, 6, 0, location).UnixMilli()
	if value.Int64() != want {
		t.Fatalf("parseDate = %d, want %d", value.Int64(), want)
	}
}

func TestJavaDateLayoutRejectsUnsupportedFields(t *testing.T) {
	t.Parallel()
	if _, err := javaDateLayout("yyyy-QQ"); err == nil {
		t.Fatal("javaDateLayout accepted unsupported quarter field")
	}
}
