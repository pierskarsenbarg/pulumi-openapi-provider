// Package main is the entry point for the parameterized openapi-provider binary.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/pkg/parameterized"
)

// version is set at build time via -ldflags "-X main.version=<ver>".
var version = "0.1.0"

func main() {
	if err := parameterized.RunParameterizedProvider(context.Background(), version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
