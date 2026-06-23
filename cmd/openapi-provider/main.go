package main

import (
	"context"
	"fmt"
	"os"

	openapi "github.com/pierskarsenbarg/pulumi-openapi-provider"
)

// version is set at build time via -ldflags "-X main.version=<ver>".
var version = "0.1.0"

func main() {
	if err := openapi.RunParameterizedProvider(context.Background(), version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
