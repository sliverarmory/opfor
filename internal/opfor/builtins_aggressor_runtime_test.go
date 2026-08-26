package opfor

import (
	"bytes"
	"context"
	"testing"
)

func TestAggressorBOFAndClientFunctionsAreInstalledInScriptRuntime(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New()
	if err != nil {
		t.Fatal(err)
	}
	program, err := CompileString("aggressor-runtime-functions.cna", `
$dispatched = 0;
$client_type = getAggressorClientType();
$packed = bof_pack("beacon", "isZ", 0x01020304, 0x0506, "A");
$dispatch_result = dispatch_event({ $dispatched++; return "discarded"; });
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtimeInstance.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = script.Unload(context.Background()) })

	if got, want := script.Get("$client_type").String(), "headless"; got != want {
		t.Fatalf("client type = %q, want %q", got, want)
	}
	wantPacked := []byte{
		0x01, 0x02, 0x03, 0x04,
		0x05, 0x06,
		0x00, 0x00, 0x00, 0x04, 0x41, 0x00, 0x00, 0x00,
	}
	packed := script.Get("$packed")
	packedBytes, ok := packed.Bytes()
	if !ok || !packed.IsBinaryString() || !bytes.Equal(packedBytes, wantPacked) {
		t.Fatalf("packed = %x/binary=%v, want %x/binary", packedBytes, packed.IsBinaryString(), wantPacked)
	}
	if got := script.Get("$dispatched").Int32(); got != 1 {
		t.Fatalf("dispatched callbacks = %d, want 1", got)
	}
	if !script.Get("$dispatch_result").IsNull() {
		t.Fatalf("dispatch result = %s, want $null", script.Get("$dispatch_result").Describe())
	}
}

func TestGetAggressorClientTypeImporterOverrideWins(t *testing.T) {
	t.Parallel()

	runtimeInstance, err := New(WithFunction("getAggressorClientType", func(context.Context, Invocation) (Value, error) {
		return String("ui"), nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtimeInstance.Invoke(context.Background(), "getAggressorClientType")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := value.String(), "ui"; got != want {
		t.Fatalf("overridden client type = %q, want %q", got, want)
	}
}

func TestBOFPackUsesPublicBeaconStringEncoderOption(t *testing.T) {
	t.Parallel()

	var seenBeacon, seenText Value
	runtimeInstance, err := New(WithBeaconStringEncoder(BeaconStringEncoderFunc(func(
		_ context.Context,
		beaconID Value,
		text Value,
	) ([]byte, error) {
		seenBeacon, seenText = beaconID, text
		return []byte{0x80, 0xff}, nil
	})))
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtimeInstance.Invoke(
		context.Background(), "bof_pack", String("beacon-custom"), String("z"), String("text"),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{0x00, 0x00, 0x00, 0x03, 0x80, 0xff, 0x00}
	got, ok := value.Bytes()
	if !ok || !value.IsBinaryString() || !bytes.Equal(got, want) {
		t.Fatalf("custom encoded bof_pack = %x/binary=%v, want %x/binary", got, value.IsBinaryString(), want)
	}
	if got, want := seenBeacon.String(), "beacon-custom"; got != want {
		t.Fatalf("encoder Beacon ID = %q, want %q", got, want)
	}
	if got, want := seenText.String(), "text"; got != want {
		t.Fatalf("encoder text = %q, want %q", got, want)
	}
}
