package spec

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/pb33f/libopenapi"
)

// Load parses an OpenAPI/Swagger spec from either a URL or local file path.
func Load(specURL, specPath string) (libopenapi.Document, error) {
	if specURL != "" {
		return loadFromURL(specURL)
	}
	if specPath != "" {
		return loadFromFile(specPath)
	}
	return nil, fmt.Errorf("either SpecURL or SpecPath must be provided")
}

func loadFromURL(url string) (libopenapi.Document, error) {
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("fetching spec from %s: %w", url, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading spec response body: %w", err)
	}
	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return nil, fmt.Errorf("parsing spec: %w", err)
	}
	return doc, nil
}

func loadFromFile(path string) (libopenapi.Document, error) {
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("reading spec file %s: %w", path, err)
	}
	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return nil, fmt.Errorf("parsing spec: %w", err)
	}
	return doc, nil
}
