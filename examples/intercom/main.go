package main

import (
	"context"
	"log"

	openapi "github.com/pierskarsenbarg/pulumi-openapi-provider"
)

func main() {
	err := openapi.RunProvider(context.Background(), "intercom", "0.1.0", openapi.Options{
		SpecURL: "https://raw.githubusercontent.com/intercom/Intercom-OpenAPI/refs/heads/main/descriptions/2.15/api.intercom.io.yaml",
	})
	if err != nil {
		log.Fatal(err)
	}
}
