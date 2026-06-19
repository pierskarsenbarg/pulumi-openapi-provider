package main

import (
	"context"
	"log"

	openapi "github.com/pierskarsenbarg/pulumi-openapi-provider"
)

func main() {
	err := openapi.RunProvider(context.Background(), "petstore", "0.1.0", openapi.Options{
		SpecURL: "https://petstore.swagger.io/v2/swagger.json",
	})
	if err != nil {
		log.Fatal(err)
	}
}
