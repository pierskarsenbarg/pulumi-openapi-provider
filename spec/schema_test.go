package spec_test

import (
	"encoding/json"
	"testing"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/spec"
)

func TestBuildSchema_IncludesLanguageMetadata(t *testing.T) {
	schemaJSON, err := spec.BuildSchema("intercom", "0.1.0", spec.DiscoveryResult{})
	if err != nil {
		t.Fatalf("BuildSchema: %v", err)
	}

	var schema struct {
		Language map[string]json.RawMessage `json:"language"`
	}
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	if len(schema.Language) != 4 {
		t.Fatalf("len(language) = %d, want 4", len(schema.Language))
	}

	var goInfo struct {
		GenerateResourceContainerTypes bool   `json:"generateResourceContainerTypes"`
		ImportBasePath                 string `json:"importBasePath"`
		RespectSchemaVersion           bool   `json:"respectSchemaVersion"`
	}
	if err := json.Unmarshal(schema.Language["go"], &goInfo); err != nil {
		t.Fatalf("unmarshal go language info: %v", err)
	}
	if !goInfo.GenerateResourceContainerTypes {
		t.Error("go.generateResourceContainerTypes = false, want true")
	}
	if goInfo.ImportBasePath != "local-package/sdk/go/intercom" {
		t.Errorf("go.importBasePath = %q, want %q", goInfo.ImportBasePath, "local-package/sdk/go/intercom")
	}
	if !goInfo.RespectSchemaVersion {
		t.Error("go.respectSchemaVersion = false, want true")
	}

	var pythonInfo struct {
		Pyproject struct {
			Enabled bool `json:"enabled"`
		} `json:"pyproject"`
		RespectSchemaVersion bool `json:"respectSchemaVersion"`
	}
	if err := json.Unmarshal(schema.Language["python"], &pythonInfo); err != nil {
		t.Fatalf("unmarshal python language info: %v", err)
	}
	if !pythonInfo.Pyproject.Enabled {
		t.Error("python.pyproject.enabled = false, want true")
	}
	if !pythonInfo.RespectSchemaVersion {
		t.Error("python.respectSchemaVersion = false, want true")
	}
}
