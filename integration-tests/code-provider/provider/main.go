package main

import (
	"context"
	"log"

	openapi "github.com/pierskarsenbarg/pulumi-openapi-provider"
)

func main() {
	err := openapi.RunProvider(context.Background(), "testapi", "0.1.0", openapi.Options{
		SpecURL: "http://localhost:3000/openapi",
	})
	if err != nil {
		log.Fatal(err)
	}
}
