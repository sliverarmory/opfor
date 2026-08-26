package opfor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"os"
	osexec "os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

// These tests pin Cobalt-Strike/sleep@60ac3ff9 BasicIO.java lines 84,
// 113, and 297-318 together with IOObject.java lines 48-83, 148-198,
// 200-249, 276-333, and 376-397. Java's charset registry is open-ended;
// OPFOR intentionally exposes the finite portable alias set tested below.
func TestSleepBasicIOEncodingAliasesAndOutputBoundaries(t *testing.T) {
	runtime, err := New(WithStdout(io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	tests := []struct {
		aliases []string
		want    string
	}{
		{[]string{"UTF-8", "UTF8", "unicode-1-1-utf-8", "uTf-8"}, "41c3a9e282ac5a0ae980"},
		{[]string{"US-ASCII", "ASCII", "646", "ANSI_X3.4-1968", "ascii7", "iso_646.irv:1983"}, "413f3f5a0ae980"},
		{[]string{"ISO-8859-1", "ISO8859_1", "latin1"}, "41e93f5a0ae980"},
		{[]string{"windows-1252", "Cp1252", "cp5348", "ibm-1252", "ibm1252"}, "41e9805a0ae980"},
		{[]string{"UTF-16", "UTF16", "Unicode", "UnicodeBig"}, "feff004100e920ac005a000ae980"},
		{[]string{"UTF-16BE", "UnicodeBigUnmarked", "ISO-10646-UCS-2"}, "004100e920ac005a000ae980"},
		{[]string{"UTF-16LE", "UnicodeLittleUnmarked"}, "4100e900ac205a000a00e980"},
	}
	for _, test := range tests {
		for _, alias := range test.aliases {
			t.Run(alias, func(t *testing.T) {
				handle := mustCallIOBuiltin(t, runtime, functions, "allocate")
				mustCallIOBuiltin(t, runtime, functions, "setEncoding", handle, String(alias))
				mustInvokeEncoding(t, runtime, "print", handle, String("Aé€"))
				mustInvokeEncoding(t, runtime, "println", handle, String("Z"))
				mustCallIOBuiltin(t, runtime, functions, "writeb", handle, BinaryString([]byte{0xe9}))
				mustCallIOBuiltin(t, runtime, functions, "bwrite", handle, String("B"), Int(0x80))
				mustCallIOBuiltin(t, runtime, functions, "closef", handle)
				got := fmt.Sprintf("%x", []byte(mustCallIOBuiltin(t, runtime, functions, "readb", handle, Int(-1)).String()))
				if got != test.want {
					t.Fatalf("encoded bytes = %s, want %s", got, test.want)
				}
			})
		}
	}

	// The portable default is explicitly UTF-8, and the shared console handle
	// uses the same encoder as explicit print/println destinations.
	defaultHandle := mustCallIOBuiltin(t, runtime, functions, "allocate")
	mustInvokeEncoding(t, runtime, "print", defaultHandle, String("é"))
	mustCallIOBuiltin(t, runtime, functions, "closef", defaultHandle)
	if got := fmt.Sprintf("%x", []byte(mustCallIOBuiltin(t, runtime, functions, "readb", defaultHandle, Int(-1)).String())); got != "c3a9" {
		t.Fatalf("default text encoding = %s, want UTF-8 c3a9", got)
	}

	var consoleOutput bytes.Buffer
	consoleRuntime, err := New(WithStdout(&consoleOutput))
	if err != nil {
		t.Fatalf("console New: %v", err)
	}
	consoleFunctions := consoleRuntime.ioFunctions()
	console := mustCallIOBuiltin(t, consoleRuntime, consoleFunctions, "getConsole")
	mustCallIOBuiltin(t, consoleRuntime, consoleFunctions, "setEncoding", console, String("ISO-8859-1"))
	mustInvokeEncoding(t, consoleRuntime, "println", String("é"))
	if got := fmt.Sprintf("%x", consoleOutput.Bytes()); got != "e90a" {
		t.Fatalf("encoded console println = %s, want e90a", got)
	}

	flushed := &encodingFlushWriter{}
	flushRuntime, err := New(WithStdout(flushed))
	if err != nil {
		t.Fatalf("flush New: %v", err)
	}
	mustInvokeEncoding(t, flushRuntime, "print")
	mustInvokeEncoding(t, flushRuntime, "print", String("x"))
	if flushed.flushes != 2 || flushed.String() != "x" {
		t.Fatalf("print flushes/output = (%d, %q), want (2, x)", flushed.flushes, flushed.String())
	}
}

func TestSleepBasicIOEncodingDecoderReplacementAndReadAhead(t *testing.T) {
	runtime, err := New(WithStdout(io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	tests := []struct {
		name    string
		charset string
		input   []byte
		want    []uint16
	}{
		{"utf8-malformed", "UTF-8", []byte{0x41, 0x80, 0xe2, 0x28, 0xa1, 0xe2, 0x82}, []uint16{0x41, 0xfffd, 0xfffd, 0x28, 0xfffd, 0xfffd}},
		{"utf8-surrogate-spelling", "UTF8", []byte{0xed, 0xa0, 0x80}, []uint16{0xfffd}},
		{"utf8-overlong", "UTF8", []byte{0xe0, 0x80, 0x80}, []uint16{0xfffd, 0xfffd, 0xfffd}},
		{"utf8-out-of-range", "UTF8", []byte{0xf4, 0x90, 0x80, 0x80}, []uint16{0xfffd, 0xfffd, 0xfffd, 0xfffd}},
		{"utf8-overlong-short", "UTF8", []byte{0xe0, 0x80, 0x41}, []uint16{0xfffd, 0xfffd, 0x41}},
		{"utf8-four-byte-overlong-short", "UTF8", []byte{0xf0, 0x80, 0x80, 0x41}, []uint16{0xfffd, 0xfffd, 0xfffd, 0x41}},
		{"utf8-out-of-range-short", "UTF8", []byte{0xf4, 0x90, 0x41}, []uint16{0xfffd, 0xfffd, 0x41}},
		{"ascii", "ASCII", []byte{0x41, 0x80, 0xff}, []uint16{0x41, 0xfffd, 0xfffd}},
		{"latin1", "latin1", []byte{0x41, 0x80, 0xff}, []uint16{0x41, 0x80, 0xff}},
		{"windows-undefined", "Cp1252", []byte{0x80, 0x81, 0x8d, 0x9f}, []uint16{0x20ac, 0xfffd, 0xfffd, 0x0178}},
		{"utf16be-pair-and-odd", "UTF-16BE", []byte{0x00, 0x41, 0xd8, 0x3d, 0xde, 0x00, 0x00}, []uint16{0x41, 0xd83d, 0xde00, 0xfffd}},
		{"utf16be-high-and-bmp", "UTF-16BE", []byte{0xd8, 0x00, 0x00, 0x41}, []uint16{0xfffd}},
		{"utf16be-high-high-low", "UTF-16BE", []byte{0xd8, 0x00, 0xd8, 0x01, 0xdc, 0x00}, []uint16{0xfffd, 0xfffd}},
		{"utf16be-high-and-odd", "UTF-16BE", []byte{0xd8, 0x00, 0x00}, []uint16{0xfffd}},
		{"utf16-bom", "UTF-16", []byte{0xff, 0xfe, 0x41, 0x00}, []uint16{0x41}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handle := readableMemoryHandle(t, runtime, functions, string(test.input))
			mustCallIOBuiltin(t, runtime, functions, "setEncoding", handle, String(test.charset))
			if got := readEncodingUnits(t, runtime, functions, handle); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("readc units = %04x, want %04x", got, test.want)
			}
		})
	}

	supplementary := readableMemoryHandle(t, runtime, functions, string([]byte{0xf0, 0x9f, 0x98, 0x80}))
	high := mustCallIOBuiltin(t, runtime, functions, "readc", supplementary)
	low := mustCallIOBuiltin(t, runtime, functions, "readc", supplementary)
	if got := fmt.Sprintf("%x/%x", []byte(high.String()), []byte(low.String())); got != "eda0bd/edb880" {
		t.Fatalf("supplementary readc WTF-8 = %s, want eda0bd/edb880", got)
	}
	if tail := mustCallIOBuiltin(t, runtime, functions, "readc", supplementary); !tail.IsNull() {
		t.Fatalf("supplementary EOF = %s, want null", tail.Describe())
	}
	if asc := mustInvokeEncoding(t, runtime, "asc", high); asc.Int32() != 0xd83d {
		t.Fatalf("asc(readc high surrogate) = %s, want 55357", asc.Describe())
	}
	if length := mustInvokeEncoding(t, runtime, "strlen", high); length.Int32() != 1 {
		t.Fatalf("strlen(readc high surrogate) = %s, want 1", length.Describe())
	}
	if character := mustInvokeEncoding(t, runtime, "charAt", high, Int(0)); character.String() != high.String() {
		t.Fatalf("charAt(readc high surrogate) bytes = %x, want %x", character.String(), high.String())
	}
	if unit := mustInvokeEncoding(t, runtime, "byteAt", high, Int(0)); unit.Int32() != 0xd83d {
		t.Fatalf("byteAt(readc high surrogate) = %s, want 55357", unit.Describe())
	}
	if character := mustInvokeEncoding(t, runtime, "left", high, Int(1)); character.String() != high.String() {
		t.Fatalf("left(readc high surrogate, 1) bytes = %x, want %x", character.String(), high.String())
	}
	if character := mustInvokeEncoding(t, runtime, "right", high, Int(1)); character.String() != high.String() {
		t.Fatalf("right(readc high surrogate, 1) bytes = %x, want %x", character.String(), high.String())
	}
	chr := mustInvokeEncoding(t, runtime, "chr", Int(0xd83d))
	if asc := mustInvokeEncoding(t, runtime, "asc", chr); asc.Int32() != 0xd83d {
		t.Fatalf("asc(chr(55357)) = %s, want 55357", asc.Describe())
	}
	if length := mustInvokeEncoding(t, runtime, "strlen", chr); length.Int32() != 1 {
		t.Fatalf("strlen(chr(55357)) = %s, want 1", length.Describe())
	}

	line := readableMemoryHandle(t, runtime, functions, string([]byte{0xe9, '\n'}))
	mustCallIOBuiltin(t, runtime, functions, "setEncoding", line, String("latin1"))
	if got := mustCallIOBuiltin(t, runtime, functions, "readln", line).String(); got != "é" {
		t.Fatalf("encoded readln = %q, want é", got)
	}

	supplementaryLine := readableMemoryHandle(t, runtime, functions, string([]byte{0xf0, 0x9f, 0x98, 0x80, '\n'}))
	lineValue := mustCallIOBuiltin(t, runtime, functions, "readln", supplementaryLine)
	if length := mustInvokeEncoding(t, runtime, "strlen", lineValue); length.Int32() != 2 {
		t.Fatalf("strlen(readln supplementary) = %s, want 2", length.Describe())
	}
	if highUnit := mustInvokeEncoding(t, runtime, "asc", mustInvokeEncoding(t, runtime, "charAt", lineValue, Int(0))); highUnit.Int32() != 0xd83d {
		t.Fatalf("readln supplementary high unit = %s, want 55357", highUnit.Describe())
	}
	if lowUnit := mustInvokeEncoding(t, runtime, "asc", mustInvokeEncoding(t, runtime, "charAt", lineValue, Int(1))); lowUnit.Int32() != 0xde00 {
		t.Fatalf("readln supplementary low unit = %s, want 56832", lowUnit.Describe())
	}

	small := readableMemoryHandle(t, runtime, functions, string([]byte{0x41, 0xc3, 0xa9, 0x42}))
	if got := readEncodingUnit(t, runtime, functions, small); got != 0x41 {
		t.Fatalf("small first unit = %04x, want 0041", got)
	}
	if available := mustCallIOBuiltin(t, runtime, functions, "available", small); available.Int64() != 0 {
		t.Fatalf("available after decoder read-ahead = %s, want 0", available.Describe())
	}
	mustCallIOBuiltin(t, runtime, functions, "setEncoding", small, String("latin1"))
	if got := mustCallIOBuiltin(t, runtime, functions, "readc", small); !got.IsNull() {
		t.Fatalf("buffered characters survived setEncoding: %s", got.Describe())
	}

	largeBytes := bytes.Repeat([]byte{'D'}, 9000)
	largeBytes[0] = 'A'
	largeBytes[sleepIOReadBufferSize] = 'C'
	large := readableMemoryHandle(t, runtime, functions, string(largeBytes))
	if got := readEncodingUnit(t, runtime, functions, large); got != 'A' {
		t.Fatalf("large first unit = %04x, want A", got)
	}
	if available := mustCallIOBuiltin(t, runtime, functions, "available", large); available.Int64() != 808 {
		t.Fatalf("large available after read-ahead = %s, want 808", available.Describe())
	}
	mustCallIOBuiltin(t, runtime, functions, "setEncoding", large, String("latin1"))
	if got := readEncodingUnit(t, runtime, functions, large); got != 'C' {
		t.Fatalf("unit after 8192-byte decoder switch = %04x, want C", got)
	}

	prefilled := readableMemoryHandle(t, runtime, functions, strings.Repeat("A", 9001))
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", prefilled, Int(1)).String(); got != "A" {
		t.Fatalf("prefill byte = %q, want A", got)
	}
	_ = readEncodingUnit(t, runtime, functions, prefilled)
	if available := mustCallIOBuiltin(t, runtime, functions, "available", prefilled); available.Int64() != 808 {
		t.Fatalf("available after binary-prefilled decoder read = %s, want 808", available.Describe())
	}
}

func TestSleepBasicIOEncodingBinaryMarkAndWriterInteractions(t *testing.T) {
	runtime, err := New(WithStdout(io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()

	interleaved := readableMemoryHandle(t, runtime, functions, string([]byte{'X', 0xc3, 0xa9, 'B'}))
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", interleaved, Int(1)).String(); got != "X" {
		t.Fatalf("binary prefix = %q, want X", got)
	}
	if got := readEncodingUnits(t, runtime, functions, interleaved); !reflect.DeepEqual(got, []uint16{0xe9, 'B'}) {
		t.Fatalf("units after binary prefix = %04x, want 00e9 0042", got)
	}

	split := readableMemoryHandle(t, runtime, functions, string([]byte{'X', 0xc3, 0xa9, 'B'}))
	if got := fmt.Sprintf("%x", []byte(mustCallIOBuiltin(t, runtime, functions, "readb", split, Int(2)).String())); got != "58c3" {
		t.Fatalf("split binary prefix = %s, want 58c3", got)
	}
	if got := readEncodingUnits(t, runtime, functions, split); !reflect.DeepEqual(got, []uint16{0xfffd, 'B'}) {
		t.Fatalf("units after split UTF-8 = %04x, want fffd 0042", got)
	}

	marked := readableMemoryHandle(t, runtime, functions, "abc")
	mustCallIOBuiltin(t, runtime, functions, "mark", marked)
	if got := readEncodingUnit(t, runtime, functions, marked); got != 'a' {
		t.Fatalf("marked first unit = %04x, want a", got)
	}
	mustCallIOBuiltin(t, runtime, functions, "reset", marked)
	if got := readEncodingUnits(t, runtime, functions, marked); !reflect.DeepEqual(got, []uint16{'b', 'c', 'a', 'b', 'c'}) {
		t.Fatalf("mark/reset units = %04x, want b c a b c", got)
	}

	resetThenSwitch := readableMemoryHandle(t, runtime, functions, "abc")
	mustCallIOBuiltin(t, runtime, functions, "mark", resetThenSwitch)
	_ = readEncodingUnit(t, runtime, functions, resetThenSwitch)
	mustCallIOBuiltin(t, runtime, functions, "reset", resetThenSwitch)
	mustCallIOBuiltin(t, runtime, functions, "setEncoding", resetThenSwitch, String("UTF8"))
	if got := readEncodingUnits(t, runtime, functions, resetThenSwitch); !reflect.DeepEqual(got, []uint16{'a', 'b', 'c'}) {
		t.Fatalf("reset/setEncoding units = %04x, want a b c", got)
	}

	delimiter := readableMemoryHandle(t, runtime, functions, "A|")
	if before := mustCallIOBuiltin(t, runtime, functions, "available", delimiter, String("|")); !before.Truth() {
		t.Fatal("available(handle, delimiter) = false before text read")
	}
	_ = readEncodingUnit(t, runtime, functions, delimiter)
	if count := mustCallIOBuiltin(t, runtime, functions, "available", delimiter); count.Int64() != 0 {
		t.Fatalf("available count after text read-ahead = %s, want 0", count.Describe())
	}
	if after := mustCallIOBuiltin(t, runtime, functions, "available", delimiter, String("|")); after.Truth() {
		t.Fatal("available(handle, delimiter) saw decoder-buffered delimiter")
	}

	emojiInput := readableMemoryHandle(t, runtime, functions, string([]byte{0xf0, 0x9f, 0x98, 0x80}))
	high := mustCallIOBuiltin(t, runtime, functions, "readc", emojiInput)
	low := mustCallIOBuiltin(t, runtime, functions, "readc", emojiInput)

	stateful := mustCallIOBuiltin(t, runtime, functions, "allocate")
	mustInvokeEncoding(t, runtime, "print", stateful, high)
	mustCallIOBuiltin(t, runtime, functions, "writeb", stateful, String("X"))
	mustInvokeEncoding(t, runtime, "print", stateful, low)
	mustCallIOBuiltin(t, runtime, functions, "closef", stateful)
	if got := fmt.Sprintf("%x", []byte(mustCallIOBuiltin(t, runtime, functions, "readb", stateful, Int(-1)).String())); got != "58f09f9880" {
		t.Fatalf("stateful surrogate/raw output = %s, want 58f09f9880", got)
	}

	dropped := mustCallIOBuiltin(t, runtime, functions, "allocate")
	mustInvokeEncoding(t, runtime, "print", dropped, high)
	mustCallIOBuiltin(t, runtime, functions, "setEncoding", dropped, String("UTF8"))
	mustInvokeEncoding(t, runtime, "print", dropped, low)
	mustCallIOBuiltin(t, runtime, functions, "closef", dropped)
	if got := mustCallIOBuiltin(t, runtime, functions, "readb", dropped, Int(-1)).String(); got != "?" {
		t.Fatalf("surrogate across setEncoding = %q, want ?", got)
	}

	bom := mustCallIOBuiltin(t, runtime, functions, "allocate")
	mustCallIOBuiltin(t, runtime, functions, "setEncoding", bom, String("UTF-16"))
	mustInvokeEncoding(t, runtime, "print", bom, String("A"))
	mustCallIOBuiltin(t, runtime, functions, "setEncoding", bom, String("UTF-16"))
	mustInvokeEncoding(t, runtime, "print", bom, String("B"))
	mustCallIOBuiltin(t, runtime, functions, "closef", bom)
	if got := fmt.Sprintf("%x", []byte(mustCallIOBuiltin(t, runtime, functions, "readb", bom, Int(-1)).String())); got != "feff0041feff0042" {
		t.Fatalf("UTF-16 writer replacement = %s, want feff0041feff0042", got)
	}

	utf16Stateful := mustCallIOBuiltin(t, runtime, functions, "allocate")
	mustCallIOBuiltin(t, runtime, functions, "setEncoding", utf16Stateful, String("UTF-16"))
	mustInvokeEncoding(t, runtime, "print", utf16Stateful, high)
	mustCallIOBuiltin(t, runtime, functions, "writeb", utf16Stateful, String("X"))
	mustInvokeEncoding(t, runtime, "print", utf16Stateful, low)
	mustCallIOBuiltin(t, runtime, functions, "closef", utf16Stateful)
	if got := fmt.Sprintf("%x", []byte(mustCallIOBuiltin(t, runtime, functions, "readb", utf16Stateful, Int(-1)).String())); got != "feff58d83dde00" {
		t.Fatalf("UTF-16 pending-surrogate BOM/raw output = %s, want feff58d83dde00", got)
	}

	utf16Dropped := mustCallIOBuiltin(t, runtime, functions, "allocate")
	mustCallIOBuiltin(t, runtime, functions, "setEncoding", utf16Dropped, String("UTF-16"))
	mustInvokeEncoding(t, runtime, "print", utf16Dropped, high)
	mustCallIOBuiltin(t, runtime, functions, "setEncoding", utf16Dropped, String("UTF-16"))
	mustInvokeEncoding(t, runtime, "print", utf16Dropped, low)
	mustCallIOBuiltin(t, runtime, functions, "closef", utf16Dropped)
	if got := fmt.Sprintf("%x", []byte(mustCallIOBuiltin(t, runtime, functions, "readb", utf16Dropped, Int(-1)).String())); got != "fefffefffffd" {
		t.Fatalf("UTF-16 surrogate switch output = %s, want fefffefffffd", got)
	}
}

func TestSleepBasicIOReadcEOFClosesDuplexAndConsole(t *testing.T) {
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	handle := newIOHandle("readc-duplex", left, left, true, true, false)
	runtime, err := New(WithStdout(io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	handleValue := ObjectValue(handle)
	peer := make(chan error, 1)
	go func() {
		_, writeErr := right.Write([]byte("x"))
		closeErr := right.Close()
		peer <- errorsJoinForTest(writeErr, closeErr)
	}()
	if got := mustCallIOBuiltin(t, runtime, functions, "readc", handleValue).String(); got != "x" {
		t.Fatalf("duplex readc = %q, want x", got)
	}
	eof := make(chan Value, 1)
	go func() {
		value, _ := callIOBuiltin(context.Background(), runtime, functions, "readc", handleValue)
		eof <- value
	}()
	select {
	case value := <-eof:
		if !value.IsNull() {
			t.Fatalf("duplex EOF readc = %s, want null", value.Describe())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("duplex EOF readc deadlocked while closing both directions")
	}
	select {
	case peerErr := <-peer:
		if peerErr != nil {
			t.Fatal(peerErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("duplex peer did not finish")
	}
	if _, err := handle.Write([]byte("late")); err == nil {
		t.Fatal("readc EOF left duplex writer open")
	}
	assertIOEOF(t, runtime, functions, handleValue, true)

	var output bytes.Buffer
	consoleRuntime, err := New(WithStdin(bytes.NewReader([]byte{'A'})), WithStdout(&output))
	if err != nil {
		t.Fatalf("console New: %v", err)
	}
	consoleFunctions := consoleRuntime.ioFunctions()
	console := mustCallIOBuiltin(t, consoleRuntime, consoleFunctions, "getConsole")
	mustInvokeEncoding(t, consoleRuntime, "print", String("before|"))
	if got := mustCallIOBuiltin(t, consoleRuntime, consoleFunctions, "readc").String(); got != "A" {
		t.Fatalf("console readc = %q, want A", got)
	}
	if got := mustCallIOBuiltin(t, consoleRuntime, consoleFunctions, "readc"); !got.IsNull() {
		t.Fatalf("console EOF readc = %s, want null", got.Describe())
	}
	mustInvokeEncoding(t, consoleRuntime, "print", String("implicit|"))
	mustInvokeEncoding(t, consoleRuntime, "print", console, String("explicit|"))
	if got := output.String(); got != "before|" {
		t.Fatalf("console output after readc EOF = %q, want before|", got)
	}
	assertIOEOF(t, consoleRuntime, consoleFunctions, console, true)
	// IOObject.setEncoding does not validate a name once both sides are closed.
	mustCallIOBuiltin(t, consoleRuntime, consoleFunctions, "setEncoding", console, String("NO-SUCH-CHARSET"))
}

func TestSleepBasicIOSetEncodingWarningsArityTaintAndOverride(t *testing.T) {
	runtime, err := New(WithStdout(io.Discard))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	functions := runtime.ioFunctions()
	handle := mustCallIOBuiltin(t, runtime, functions, "allocate")
	if _, err := callIOBuiltin(context.Background(), runtime, functions, "setEncoding", handle, String("NO-SUCH")); err == nil || err.Error() != "&setEncoding: specified a non-existent encoding 'NO-SUCH'" {
		t.Fatalf("invalid encoding error = %v", err)
	}
	if _, err := callIOBuiltin(context.Background(), runtime, functions, "setEncoding", handle); err == nil || err.Error() != "&setEncoding: specified a non-existent encoding ''" {
		t.Fatalf("missing encoding error = %v", err)
	}
	if _, err := callIOBuiltin(context.Background(), runtime, functions, "setEncoding", String("UTF-8")); err == nil || !strings.Contains(err.Error(), "expected I/O handle") {
		t.Fatalf("name-only setEncoding error = %v", err)
	}
	// Extra arguments are ignored after the selected name.
	mustCallIOBuiltin(t, runtime, functions, "setEncoding", handle, String("latin1"), String("NO-SUCH"))
	mustInvokeEncoding(t, runtime, "print", handle, String("é"))
	mustCallIOBuiltin(t, runtime, functions, "closef", handle)
	if got := fmt.Sprintf("%x", []byte(mustCallIOBuiltin(t, runtime, functions, "readb", handle, Int(-1)).String())); got != "e9" {
		t.Fatalf("encoding changed after invalid/extra arguments = %s, want e9", got)
	}

	closed := mustCallIOBuiltin(t, runtime, functions, "allocate")
	mustCallIOBuiltin(t, runtime, functions, "closef", closed)
	mustCallIOBuiltin(t, runtime, functions, "closef", closed)
	mustCallIOBuiltin(t, runtime, functions, "setEncoding", closed, String("NO-SUCH"))

	var warningOutput bytes.Buffer
	warningRuntime, err := New(WithStdout(&warningOutput), WithStderr(&warningOutput))
	if err != nil {
		t.Fatalf("warning New: %v", err)
	}
	const warningSource = `sub bad { setEncoding($1, "NO-SUCH"); println("unreachable"); } $h = allocate(); bad($h); checkError($why); println("continued|" . $why); print($h, "é"); closef($h); println(unpack("H*", readb($h, -1))[0]);`
	if _, err := warningRuntime.Eval(context.Background(), "-e", warningSource); err != nil {
		t.Fatalf("warning Eval: %v\n%s", err, warningOutput.String())
	}
	wantWarning := "Warning: &setEncoding: specified a non-existent encoding 'NO-SUCH' at -e:1\ncontinued|\nc3a9\n"
	if got := warningOutput.String(); got != wantWarning {
		t.Fatalf("invalid-name warning mismatch\nwant:\n%sgot:\n%s", wantWarning, got)
	}

	taintRuntime, err := New(WithTaintMode(true), WithStdout(io.Discard))
	if err != nil {
		t.Fatalf("taint New: %v", err)
	}
	taintHandle := mustInvokeEncoding(t, taintRuntime, "allocate")
	mustInvokeEncoding(t, taintRuntime, "writeb", taintHandle, String("q"))
	mustInvokeEncoding(t, taintRuntime, "closef", taintHandle)
	if value := mustInvokeEncoding(t, taintRuntime, "readc", taintHandle); value.String() != "q" || !value.IsTainted() {
		t.Fatalf("tainted readc = (%q, %t), want (q, true)", value.String(), value.IsTainted())
	}

	overrideRuntime, err := New(WithFunction("readc", func(context.Context, Invocation) (Value, error) {
		return String("importer"), nil
	}))
	if err != nil {
		t.Fatalf("override New: %v", err)
	}
	if got := mustInvokeEncoding(t, overrideRuntime, "readc").String(); got != "importer" {
		t.Fatalf("importer readc override = %q, want importer", got)
	}
}

func TestSleepBasicIOEncodingExactOutput(t *testing.T) {
	got := runPureGoBasicIOEncodingProbe(t, sleepBasicIOEncodingProbeSource, nil)
	if want := []byte(sleepBasicIOEncodingProbeOutput); !bytes.Equal(got, want) {
		t.Fatalf("encoding probe mismatch\nwant:\n%sgot:\n%s", want, got)
	}
}

// TestSleepBasicIOEncodingOfficialJARDifferential is opt-in because the
// licensed official JAR is not a repository dependency. It pins the JAR hash
// and compares aliases, encoded output, malformed replacement, UTF-16 code
// units, decoder read-ahead, binary interleaving, mark/reset, EOF closure,
// invalid-name warnings, and closed-handle invalid-name behavior.
func TestSleepBasicIOEncodingOfficialJARDifferential(t *testing.T) {
	jar := os.Getenv("OPFOR_SLEEP_JAR")
	if jar == "" {
		t.Skip("set OPFOR_SLEEP_JAR to the official Sleep 2.1 JAR for encoding differential verification")
	}
	jarBytes, err := os.ReadFile(jar)
	if err != nil {
		t.Fatal(err)
	}
	const officialSHA256 = "0ddde5e9e8d8d8d334d071b1f887c379f5d0be9b190566f05365997b3e375ff1"
	if got := fmt.Sprintf("%x", sha256.Sum256(jarBytes)); got != officialSHA256 {
		t.Fatalf("Sleep JAR SHA-256 = %s, want %s", got, officialSHA256)
	}
	java := os.Getenv("OPFOR_JAVA")
	if java == "" {
		java, err = osexec.LookPath("java")
		if err != nil {
			t.Skipf("official JAR supplied but java is unavailable: %v", err)
		}
	}

	for _, probe := range []struct {
		name   string
		source string
		stdin  []byte
	}{
		{"main", sleepBasicIOEncodingProbeSource, nil},
		{"invalid-name", sleepBasicIOEncodingInvalidProbeSource, nil},
		{"console-eof", sleepBasicIOEncodingConsoleEOFProbeSource, []byte{'A'}},
	} {
		t.Run(probe.name, func(t *testing.T) {
			goOutput := runPureGoBasicIOEncodingProbe(t, probe.source, probe.stdin)
			command := osexec.Command(java, "-jar", jar, "-e", probe.source)
			command.Stdin = bytes.NewReader(probe.stdin)
			javaOutput, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("official Sleep encoding probe: %v\n%s", err, javaOutput)
			}
			if !bytes.Equal(normalizeEncodingProbeOutput(goOutput), normalizeEncodingProbeOutput(javaOutput)) {
				t.Fatalf("official Sleep encoding output mismatch\nwant:\n%sgot:\n%s", javaOutput, goOutput)
			}
		})
	}
}

func mustInvokeEncoding(t *testing.T, runtime *Runtime, name string, values ...Value) Value {
	t.Helper()
	value, err := runtime.Invoke(context.Background(), name, values...)
	if err != nil {
		t.Fatalf("&%s: %v", name, err)
	}
	return value
}

func readEncodingUnit(t *testing.T, runtime *Runtime, functions map[string]NativeFunc, handle Value) uint16 {
	t.Helper()
	value := mustCallIOBuiltin(t, runtime, functions, "readc", handle)
	if value.IsNull() {
		t.Fatal("readc returned null before expected unit")
	}
	units := sleepFormattedUTF16Units(value.String())
	if len(units) != 1 {
		t.Fatalf("readc value %q contains %d UTF-16 units, want 1", value.String(), len(units))
	}
	return units[0]
}

func readEncodingUnits(t *testing.T, runtime *Runtime, functions map[string]NativeFunc, handle Value) []uint16 {
	t.Helper()
	var units []uint16
	for {
		value := mustCallIOBuiltin(t, runtime, functions, "readc", handle)
		if value.IsNull() {
			return units
		}
		decoded := sleepFormattedUTF16Units(value.String())
		if len(decoded) != 1 {
			t.Fatalf("readc value %q contains %d UTF-16 units, want 1", value.String(), len(decoded))
		}
		units = append(units, decoded[0])
	}
}

func errorsJoinForTest(first, second error) error {
	if first != nil {
		return first
	}
	return second
}

type encodingFlushWriter struct {
	bytes.Buffer
	flushes int
}

func (writer *encodingFlushWriter) Flush() error {
	writer.flushes++
	return nil
}

func runPureGoBasicIOEncodingProbe(t *testing.T, source string, stdin []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	runtime, err := New(WithStdin(bytes.NewReader(stdin)), WithStdout(&output), WithStderr(&output))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := runtime.Eval(context.Background(), "-e", source); err != nil {
		t.Fatalf("pure-Go encoding probe: %v\n%s", err, output.String())
	}
	return output.Bytes()
}

func normalizeEncodingProbeOutput(output []byte) []byte {
	lines := strings.SplitAfter(string(output), "\n")
	for index, line := range lines {
		marker := strings.LastIndex(line, " at -e:")
		if marker < 0 || !strings.HasPrefix(line, "Warning: ") {
			continue
		}
		newline := ""
		if strings.HasSuffix(line, "\n") {
			newline = "\n"
		}
		lines[index] = line[:marker] + " at -e:<line>" + newline
	}
	return []byte(strings.Join(lines, ""))
}

const sleepBasicIOEncodingProbeSource = `sub unit {
  if ($1 is $null) { return "-"; }
  return unpack("H*", pack("U1", $1))[0];
}
sub encoded {
  $h = allocate();
  setEncoding($h, $1);
  print($h, "Aé€");
  println($h, "Z");
  writeb($h, pack("H*", "e9"));
  bwrite($h, "B", 128);
  closef($h);
  return unpack("H*", readb($h, -1))[0];
}
sub decoded4 {
  $decode = allocate(); writeb($decode, pack("H*", $1)); closef($decode); setEncoding($decode, $2);
  $d1 = readc($decode); $d2 = readc($decode); $d3 = readc($decode); $d4 = readc($decode);
  return unit($d1) . "," . unit($d2) . "," . unit($d3) . "," . unit($d4);
}
println("utf8:" . encoded("unicode-1-1-utf-8"));
println("ascii:" . encoded("ASCII"));
println("latin1:" . encoded("ISO8859_1"));
println("cp1252:" . encoded("Cp1252"));
println("utf16:" . encoded("UTF-16"));
println("utf16be:" . encoded("UnicodeBigUnmarked"));
println("utf16le:" . encoded("UnicodeLittleUnmarked"));
$bad = allocate(); writeb($bad, pack("H*", "4180e228a1e282")); closef($bad); setEncoding($bad, "UTF8");
$b1 = readc($bad); $b2 = readc($bad); $b3 = readc($bad); $b4 = readc($bad); $b5 = readc($bad); $b6 = readc($bad); $b7 = readc($bad);
println("malformed:" . unit($b1) . "," . unit($b2) . "," . unit($b3) . "," . unit($b4) . "," . unit($b5) . "," . unit($b6) . "," . unit($b7));
println("utf8-edge:" . decoded4("e08041", "UTF8") . "|" . decoded4("f0808041", "UTF8") . "|" . decoded4("f49041", "UTF8"));
println("utf16-edge:" . decoded4("d8000041", "UTF-16BE") . "|" . decoded4("d800d801dc00", "UTF-16BE") . "|" . decoded4("d80000", "UTF-16BE"));
$supp = allocate(); writeb($supp, pack("H*", "f09f9880")); closef($supp);
$s1 = readc($supp); $s2 = readc($supp); $s3 = readc($supp);
println("supplementary:" . unit($s1) . "," . unit($s2) . "," . unit($s3));
println("char-api:" . asc($s1) . "," . strlen($s1) . "," . asc($s2) . "," . strlen($s2) . "," . unit(charAt($s1, 0)) . "," . byteAt($s1, 0) . "," . unit(left($s1, 1)) . "," . unit(right($s1, 1)) . "," . unit(chr(55357)) . "," . strlen(chr(55357)) . "," . asc(chr(55357)));
$utf16state = allocate(); setEncoding($utf16state, "UTF-16"); print($utf16state, $s1); writeb($utf16state, "X"); print($utf16state, $s2); closef($utf16state);
$utf16drop = allocate(); setEncoding($utf16drop, "UTF-16"); print($utf16drop, $s1); setEncoding($utf16drop, "UTF-16"); print($utf16drop, $s2); closef($utf16drop);
println("utf16-state:" . unpack("H*", readb($utf16state, -1))[0] . "," . unpack("H*", readb($utf16drop, -1))[0]);
$large = allocate(); writeb($large, ("A" x 8192) . "C" . ("D" x 807)); closef($large); setEncoding($large, "UTF8");
$l1 = readc($large); $la = available($large); setEncoding($large, "latin1"); $l2 = readc($large);
println("read-ahead:" . unit($l1) . "," . $la . "," . unit($l2));
$prefilled = allocate(); writeb($prefilled, "A" x 9001); closef($prefilled); $rawfirst = readb($prefilled, 1); $p1 = readc($prefilled); $pa = available($prefilled);
println("prefilled:" . $rawfirst . "," . unit($p1) . "," . $pa);
$split = allocate(); writeb($split, pack("H*", "58c3a942")); closef($split); setEncoding($split, "UTF8");
$prefix = unpack("H*", readb($split, 2))[0]; $sp1 = readc($split); $sp2 = readc($split); $sp3 = readc($split);
println("binary:" . $prefix . "," . unit($sp1) . "," . unit($sp2) . "," . unit($sp3));
$marked = allocate(); writeb($marked, "abc"); closef($marked); mark($marked); $m1 = readc($marked); reset($marked);
$m2 = readc($marked); $m3 = readc($marked); $m4 = readc($marked); $m5 = readc($marked); $m6 = readc($marked); $m7 = readc($marked);
println("mark:" . unit($m1) . "," . unit($m2) . "," . unit($m3) . "," . unit($m4) . "," . unit($m5) . "," . unit($m6) . "," . unit($m7));
$closed = allocate(); writeb($closed, "x"); closef($closed); $c1 = readc($closed); $before = "open"; if (-eof $closed) { $before = "closed"; } $c2 = readc($closed); $after = "open"; if (-eof $closed) { $after = "closed"; }
println("eof:" . unit($c1) . "," . $before . "," . unit($c2) . "," . $after);
`

const sleepBasicIOEncodingProbeOutput = "utf8:41c3a9e282ac5a0ae980\n" +
	"ascii:413f3f5a0ae980\n" +
	"latin1:41e93f5a0ae980\n" +
	"cp1252:41e9805a0ae980\n" +
	"utf16:feff004100e920ac005a000ae980\n" +
	"utf16be:004100e920ac005a000ae980\n" +
	"utf16le:4100e900ac205a000a00e980\n" +
	"malformed:0041,fffd,fffd,0028,fffd,fffd,-\n" +
	"utf8-edge:fffd,fffd,0041,-|fffd,fffd,fffd,0041|fffd,fffd,0041,-\n" +
	"utf16-edge:fffd,-,-,-|fffd,fffd,-,-|fffd,-,-,-\n" +
	"supplementary:d83d,de00,-\n" +
	"char-api:55357,1,56832,1,d83d,55357,d83d,d83d,d83d,1,55357\n" +
	"utf16-state:feff58d83dde00,fefffefffffd\n" +
	"read-ahead:0041,808,0043\n" +
	"prefilled:A,0041,808\n" +
	"binary:58c3,fffd,0042,-\n" +
	"mark:0061,0062,0063,0061,0062,0063,-\n" +
	"eof:0078,open,-,closed\n"

const sleepBasicIOEncodingInvalidProbeSource = `sub bad { setEncoding($1, "NO-SUCH"); println("unreachable"); } $h = allocate(); bad($h); checkError($why); println("continued|" . $why); $closed = allocate(); closef($closed); closef($closed); setEncoding($closed, "NO-SUCH"); println("closed-ok");`

const sleepBasicIOEncodingConsoleEOFProbeSource = `print("before|"); $first = readc(); $eof = readc(); print("implicit|"); print(getConsole(), "explicit|");`
