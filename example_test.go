package opfor_test

import (
	"bytes"
	"context"
	"fmt"

	"github.com/sliverarmory/opfor"
)

func Example() {
	program, err := opfor.CompileString("hello.cna", `
sub greeting {
    return "hello " . $1;
}
println(greeting(@ARGV[0]));
`)
	if err != nil {
		panic(err)
	}

	var output bytes.Buffer
	runtime, err := opfor.New(opfor.WithStdout(&output))
	if err != nil {
		panic(err)
	}
	if _, err := runtime.Execute(context.Background(), program, opfor.String("operator")); err != nil {
		panic(err)
	}
	fmt.Print(output.String())
	// Output: hello operator
}

func ExampleAggressorBeaconActionProvider() {
	var tasks bytes.Buffer
	provider := opfor.AggressorBeaconActionProviderFunc(func(
		_ context.Context,
		action opfor.AggressorBeaconAction,
	) error {
		fmt.Fprintf(
			&tasks,
			"%s beacon=%s command=%s\n",
			action.Name,
			action.Target.String(),
			action.Arguments[0].String(),
		)
		return nil
	})

	var output bytes.Buffer
	runtime, err := opfor.New(
		opfor.WithStdout(&output),
		opfor.WithAggressorBeaconActionProvider(provider),
	)
	if err != nil {
		panic(err)
	}
	defer runtime.Close(context.Background())

	program, err := opfor.CompileString("task.cna", `
bshell("42", "whoami");
println("script continued");
`)
	if err != nil {
		panic(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		panic(err)
	}

	fmt.Print(tasks.String())
	fmt.Print(output.String())
	// Output:
	// bshell beacon=42 command=whoami
	// script continued
}

func ExampleCallableFunc() {
	callback := opfor.CallableFunc(func(_ context.Context, values ...opfor.Value) (opfor.Value, error) {
		return opfor.String("hello " + values[0].String()), nil
	})
	runtime, err := opfor.New(opfor.WithInitialGlobals(map[string]opfor.Value{
		"greeter": opfor.FunctionValue(callback),
	}))
	if err != nil {
		panic(err)
	}
	defer runtime.Close(context.Background())

	result, err := runtime.Eval(context.Background(), "callback.sl", `[$greeter: "operator"]`)
	if err != nil {
		panic(err)
	}
	fmt.Println(result.String())
	// Output: hello operator
}
