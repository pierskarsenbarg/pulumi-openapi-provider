package spec

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/pb33f/libopenapi"
)

// Load parses an OpenAPI/Swagger spec from either a URL or local file path.
// client is used for URL fetches; pass nil to use http.DefaultClient.
func Load(specURL, specPath string, client *http.Client) (libopenapi.Document, error) {
	if specURL != "" {
		return loadFromURL(specURL, client)
	}
	if specPath != "" {
		return loadFromFile(specPath)
	}
	return nil, fmt.Errorf("either SpecURL or SpecPath must be provided")
}

// LoadSpec parses an OpenAPI spec from a URL or file path, detecting the source
// by prefix: http:// and https:// are fetched over HTTP, file:// URIs have the
// prefix stripped and read from disk, and anything else is treated as a local
// file path (absolute or relative).
func LoadSpec(src string) (libopenapi.Document, error) {
	switch {
	case strings.HasPrefix(src, "http://"), strings.HasPrefix(src, "https://"):
		return loadFromURL(src, nil)
	case strings.HasPrefix(src, "file://"):
		return loadFromFile(strings.TrimPrefix(src, "file://"))
	default:
		return loadFromFile(src)
	}
}

func loadFromURL(url string, client *http.Client) (libopenapi.Document, error) {
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Get(url) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("fetching spec from %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading spec response body: %w", err)
	}
	return loadFromBytes(data)
}

func loadFromFile(path string) (libopenapi.Document, error) {
	data, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("reading spec file %s: %w", path, err)
	}
	return loadFromBytes(data)
}

func loadFromBytes(data []byte) (libopenapi.Document, error) {
	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return nil, fmt.Errorf("parsing spec: %w", err)
	}
	switch doc.GetSpecInfo().SpecFormat {
	case specFormatOAS2, "oas3", "oas3_1", "oas3_2":
		return doc, nil
	default:
		return nil, fmt.Errorf("not a recognised OpenAPI/Swagger spec (spec format %q)", doc.GetSpecInfo().SpecFormat)
	}
}
