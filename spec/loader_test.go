package spec_test

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/spec"
)

func TestLoad_RejectsNonOpenAPIDocument(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/not-openapi.yaml"
	if err := os.WriteFile(path, []byte("foo: bar\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := spec.Load("", path, nil)
	if err == nil {
		t.Fatal("expected error for non-OpenAPI document, got nil")
	}
}

func TestLoad_AcceptsSwagger2Document(t *testing.T) {
	swagger2 := []byte(`swagger: "2.0"
info:
  title: Test API
  version: "1.0"
paths: {}
`)
	tmp := t.TempDir()
	path := tmp + "/swagger.yaml"
	if err := os.WriteFile(path, swagger2, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := spec.Load("", path, nil)
	if err != nil {
		t.Fatalf("expected no error for valid Swagger 2.0 spec, got: %v", err)
	}
}

func TestLoad_AcceptsOpenAPI3Document(t *testing.T) {
	oas3 := []byte(`openapi: "3.0.0"
info:
  title: Test API
  version: "1.0"
paths: {}
`)
	tmp := t.TempDir()
	path := tmp + "/oas3.yaml"
	if err := os.WriteFile(path, oas3, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := spec.Load("", path, nil)
	if err != nil {
		t.Fatalf("expected no error for valid OAS3 spec, got: %v", err)
	}
}

var simpleSwaggerSpec = []byte(`swagger: "2.0"
info:
  title: Test API
  version: "1.0"
paths: {}
`)

func TestLoad_URL_UsesNilClientSuccessfully(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(simpleSwaggerSpec)
	}))
	defer srv.Close()

	_, err := spec.Load(srv.URL, "", nil)
	if err != nil {
		t.Fatalf("expected no error with nil client, got: %v", err)
	}
}

func TestLoad_URL_UsesProvidedClient(t *testing.T) {
	requested := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = true
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(simpleSwaggerSpec)
	}))
	defer srv.Close()

	seen := false
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			seen = true
			return http.DefaultTransport.RoundTrip(r)
		}),
	}

	_, err := spec.Load(srv.URL, "", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !requested {
		t.Fatal("test server was never called")
	}
	if !seen {
		t.Fatal("custom transport was not used")
	}
}

func TestLoad_URL_PropagatesClientError(t *testing.T) {
	client := &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return nil, &net.OpError{Op: "dial", Err: fmt.Errorf("connection refused")}
		}),
	}

	_, err := spec.Load("http://127.0.0.1:1/openapi.yaml", "", client)
	if err == nil {
		t.Fatal("expected error from failing client, got nil")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
