package opfor

import (
	"context"
	"errors"
	"reflect"
	"regexp"
	"testing"
)

func TestPortableJavaUUIDOpenJDKValueVectors(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "uuid-vectors.sl", `
$parsed = [UUID fromString: "f47ac10b-58cc-11cf-a447-001122334455"];
$constructed = [new java.util.UUID: -830138926817865265L, -6609313854554684331L];
$short = [UUID fromString: "1-2-3-4-5"];
$zero = [UUID fromString: "00000000-0000-0000-0000-000000000000"];
$negative = [UUID fromString: "ffffffff-ffff-ffff-ffff-ffffffffffff"];
return @(
	[$parsed toString],
	[$constructed toString],
	[$short toString],
	[[$parsed getClass] getName],
	$parsed isa ^UUID,
	$parsed isa ^java.lang.Comparable,
	$parsed isa ^java.io.Serializable,
	[$parsed getMostSignificantBits],
	[$parsed getLeastSignificantBits],
	[$parsed version],
	[$parsed variant],
	[$parsed timestamp],
	[$parsed clockSequence],
	[$parsed node],
	[$parsed hashCode],
	[$parsed equals: $constructed],
	[$parsed equals: "not-a-uuid"],
	[$parsed compareTo: $constructed],
	[$zero compareTo: $negative]
);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	want := []string{
		"f47ac10b-58cc-11cf-a447-001122334455",
		"f47ac10b-58cc-11cf-a447-001122334455",
		"00000001-0002-0003-0004-000000000005",
		"java.util.UUID", "1", "1", "1",
		"-830138926817865265", "-6609313854554684331",
		"1", "2", "130420551515291915", "9287", "73588229205",
		"717395072", "1", "", "0", "1",
	}
	if got := argvValueStrings(array.Values()); !reflect.DeepEqual(got, want) {
		t.Fatalf("UUID OpenJDK vectors = %q, want %q", got, want)
	}
}

func TestPortableJavaUUIDAuthoredFactoryAndCoercionShape(t *testing.T) {
	t.Parallel()

	// This OPFOR-authored snippet covers explicit toString use, ordinary object
	// interpolation, and the deterministic name-based factory without importing
	// executable evidence from an external .cna project.
	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "uuid-factory-coercion.sl", `
import java.util.UUID;

sub get_random_temp_filename {
	local('$tmpdir $random');
	$tmpdir = "/tmp/";
	$random = [java.util.UUID randomUUID];
	return $tmpdir . [$random toString];
}

$pipename = [java.util.UUID randomUUID];
$named = [UUID nameUUIDFromBytes: cast("hello", "b")];
return @(get_random_temp_filename(), "$pipename", [$pipename version], [$pipename variant],
	[$named toString], [$named version], [$named variant]);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	got := argvValueStrings(array.Values())
	if len(got) != 7 {
		t.Fatalf("UUID factory/coercion result = %q", got)
	}
	canonical := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if len(got[0]) < len("/tmp/") || got[0][:len("/tmp/")] != "/tmp/" || !canonical.MatchString(got[0][len("/tmp/"):]) {
		t.Fatalf("temporary UUID = %q", got[0])
	}
	if !canonical.MatchString(got[1]) {
		t.Fatalf("coerced UUID = %q", got[1])
	}
	if got[2] != "4" || got[3] != "2" {
		t.Fatalf("random UUID version/variant = %q/%q, want 4/2", got[2], got[3])
	}
	if got[4] != "5d41402a-bc4b-3a76-b971-9d911017c592" || got[5] != "3" || got[6] != "2" {
		t.Fatalf("name UUID vector = %q/%q/%q", got[4], got[5], got[6])
	}
}

func TestPortableJavaUUIDJavaSoftExceptions(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
	value, err := runtimeInstance.Eval(context.Background(), "uuid-errors.sl", `
$bad = [UUID fromString: "not-a-uuid"];
$parse_error = checkError();
$bad_hex = [UUID fromString: "g0000000-0000-0000-0000-000000000000"];
$hex_error = checkError();
$empty_group = [UUID fromString: "1--3-4-5"];
$empty_group_error = checkError();
$random = [UUID fromString: "f47ac10b-58cc-41cf-a447-001122334455"];
$timestamp = [$random timestamp];
$time_error = checkError();
$comparison = [$random compareTo: "not-a-uuid"];
$compare_error = checkError();
$null_parse = [UUID fromString: $null];
$null_parse_error = checkError();
$null_comparison = [$random compareTo: $null];
$null_compare_error = checkError();
return @(
	$bad, [[$parse_error getClass] getName], [$parse_error getMessage],
	$bad_hex, [[$hex_error getClass] getName], [$hex_error getMessage],
	$empty_group, [[$empty_group_error getClass] getName], [$empty_group_error getMessage],
	$timestamp, [[$time_error getClass] getName], [$time_error getMessage],
	$comparison, [[$compare_error getClass] getName],
	$null_parse, [[$null_parse_error getClass] getName],
	$null_comparison, [[$null_compare_error getClass] getName]
);
`)
	if err != nil {
		t.Fatal(err)
	}
	array, ok := value.Array()
	if !ok {
		t.Fatalf("result = %s, want array", value.Describe())
	}
	want := []string{
		"", "java.lang.IllegalArgumentException", "Invalid UUID string: not-a-uuid",
		"", "java.lang.NumberFormatException", `Error at index 0 in: "g0000000"`,
		"", "java.lang.NumberFormatException", `For input string: "" under radix 16`,
		"", "java.lang.UnsupportedOperationException", "Not a time-based UUID",
		"", "java.lang.ClassCastException",
		"", "java.lang.NullPointerException", "", "java.lang.NullPointerException",
	}
	if got := argvValueStrings(array.Values()); !reflect.DeepEqual(got, want) {
		t.Fatalf("UUID soft errors = %q, want %q", got, want)
	}
}

func TestPortableJavaUUIDPreservesObjectHostPrecedence(t *testing.T) {
	t.Parallel()

	t.Run("constructor handled", func(t *testing.T) {
		host := ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
			if invocation.Op == ObjectConstruct && resolvePortableClassName(invocation.Class) == "java.util.UUID" {
				return String("importer-constructor"), nil
			}
			return Null(), &UnsupportedError{Operation: "object"}
		})
		runtimeInstance, err := New(WithObjectHost(host))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		value, err := runtimeInstance.Eval(context.Background(), "uuid-host-constructor.sl", `return [new UUID: 0L, 0L];`)
		if err != nil || value.String() != "importer-constructor" {
			t.Fatalf("constructor override = (%s, %v)", value.Describe(), err)
		}
	})

	t.Run("static handled", func(t *testing.T) {
		host := ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
			if invocation.Op == ObjectInvoke && resolvePortableClassName(invocation.Class) == "java.util.UUID" && invocation.Message == "randomUUID" {
				return String("importer-static"), nil
			}
			return Null(), &UnsupportedError{Operation: "object"}
		})
		runtimeInstance, err := New(WithObjectHost(host))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		value, err := runtimeInstance.Eval(context.Background(), "uuid-host-static.sl", `return [UUID randomUUID];`)
		if err != nil || value.String() != "importer-static" {
			t.Fatalf("static override = (%s, %v)", value.Describe(), err)
		}
	})

	t.Run("unsupported falls through and method handled", func(t *testing.T) {
		host := ObjectHostFunc(func(_ context.Context, invocation ObjectInvocation) (Value, error) {
			if _, ok := portableJavaUUIDValue(invocation.Target); ok && invocation.Op == ObjectInvoke && invocation.Message == "toString" {
				return String("importer-method"), nil
			}
			return Null(), &UnsupportedError{Operation: "object"}
		})
		runtimeInstance, err := New(WithObjectHost(host))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		value, err := runtimeInstance.Eval(context.Background(), "uuid-host-method.sl", `
$uuid = [UUID fromString: "00000000-0000-0000-0000-000000000001"];
return @([$uuid toString], [$uuid version]);
`)
		if err != nil {
			t.Fatal(err)
		}
		array, ok := value.Array()
		if !ok {
			t.Fatalf("result = %s, want array", value.Describe())
		}
		if got, want := argvValueStrings(array.Values()), []string{"importer-method", "0"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("method precedence/fallback = %q, want %q", got, want)
		}
	})

	t.Run("fatal importer error", func(t *testing.T) {
		wantErr := errors.New("importer rejected UUID")
		host := ObjectHostFunc(func(context.Context, ObjectInvocation) (Value, error) {
			return Null(), wantErr
		})
		runtimeInstance, err := New(WithObjectHost(host))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = runtimeInstance.Close(context.Background()) })
		_, evalErr := runtimeInstance.Eval(context.Background(), "uuid-host-fatal.sl", `return [UUID randomUUID];`)
		if !errors.Is(evalErr, wantErr) {
			t.Fatalf("fatal importer error = %v, want %v", evalErr, wantErr)
		}
	})
}
