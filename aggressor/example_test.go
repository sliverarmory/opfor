package aggressor_test

import (
	"bytes"
	"context"
	"fmt"

	"github.com/sliverarmory/opfor"
	"github.com/sliverarmory/opfor/aggressor"
)

func ExampleHost() {
	host := aggressor.NewHost()
	if err := host.Register("operator_name", func(_ context.Context, _ aggressor.Request) (aggressor.Value, error) {
		return opfor.String("alice"), nil
	}); err != nil {
		panic(err)
	}

	var output bytes.Buffer
	runtime, err := opfor.New(opfor.WithHost(host), opfor.WithStdout(&output))
	if err != nil {
		panic(err)
	}
	program, err := opfor.CompileString("host.cna", `println(operator_name());`)
	if err != nil {
		panic(err)
	}
	if _, err := runtime.Execute(context.Background(), program); err != nil {
		panic(err)
	}
	fmt.Print(output.String())
	// Output: alice
}
