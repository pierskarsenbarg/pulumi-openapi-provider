package spec_test

import (
	"os"
	"testing"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/spec"
)

func TestLoad_RejectsNonOpenAPIDocument(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/not-openapi.yaml"
	if err := os.WriteFile(path, []byte("foo: bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := spec.Load("", path)
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
	if err := os.WriteFile(path, swagger2, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := spec.Load("", path)
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
	if err := os.WriteFile(path, oas3, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := spec.Load("", path)
	if err != nil {
		t.Fatalf("expected no error for valid OAS3 spec, got: %v", err)
	}
}
