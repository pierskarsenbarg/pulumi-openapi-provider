package main

import (
	"context"
	"log"
	"net/http"

	openapi "github.com/pierskarsenbarg/pulumi-openapi-provider"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func main() {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		loadingRules,
		&clientcmd.ConfigOverrides{},
	)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		log.Fatal(err)
	}

	transport, err := rest.TransportFor(restConfig)
	if err != nil {
		log.Fatal(err)
	}

	err = openapi.RunProvider(context.Background(), "openapi-k8s", "0.1.0", openapi.Options{
		SpecURL:    restConfig.Host + "/openapi/v2",
		BaseURL:    restConfig.Host,
		HTTPClient: &http.Client{Transport: transport},
		Overrides: map[string]openapi.ResourceOverride{
			"*": {IDField: "metadata.name"},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
