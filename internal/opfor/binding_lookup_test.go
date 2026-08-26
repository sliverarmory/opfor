package opfor

import (
	"context"
	"errors"
	"testing"
)

func TestInvokeBindingByIDSelectsExactRegistration(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	program, err := runtime.CompileString("binding-id.cna", `
command exact { return "first"; }
command exact { return "second"; }
`)
	if err != nil {
		t.Fatal(err)
	}
	script, err := runtime.Load(context.Background(), program)
	if err != nil {
		t.Fatal(err)
	}
	bindings := runtime.Bindings(BindingCommand, "exact")
	if len(bindings) != 2 {
		t.Fatalf("bindings = %#v, want two", bindings)
	}

	value, err := runtime.InvokeBinding(context.Background(), BindingCommand, "exact")
	if err != nil || value.String() != "second" {
		t.Fatalf("newest binding = %s, %v; want second", value.Describe(), err)
	}
	value, err = runtime.InvokeBindingByID(context.Background(), script.ID(), bindings[0].ID)
	if err != nil || value.String() != "first" {
		t.Fatalf("first binding by ID = %s, %v; want first", value.Describe(), err)
	}
	metadata, ok := runtime.BindingByID(script.ID(), bindings[1].ID)
	if !ok || metadata.Name != "exact" || metadata.ID != bindings[1].ID {
		t.Fatalf("BindingByID = %#v, %v", metadata, ok)
	}
}

func TestInvokeBindingByIDRejectsUnknownIdentity(t *testing.T) {
	t.Parallel()

	runtime, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close(context.Background()) })
	_, err = runtime.InvokeBindingByID(context.Background(), 7, 11)
	var unsupported *UnsupportedError
	if !errors.As(err, &unsupported) || unsupported.Operation != "binding id" || unsupported.Name != "7/11" {
		t.Fatalf("unknown binding error = %#v", err)
	}
}
