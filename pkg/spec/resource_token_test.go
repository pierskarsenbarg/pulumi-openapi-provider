package spec_test

import (
	"os"
	"strings"
	"testing"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/pkg/spec"
)

const twoResourceOAS3 = `{
  "openapi": "3.0.0",
  "info": {"title": "Test", "version": "1.0"},
  "servers": [{"url": "https://api.example.com"}],
  "paths": {
    "/widgets": {
      "post": {
        "requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}}},
        "responses": {"201": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}}}}
      }
    },
    "/widgets/{widgetId}": {
      "get": {
        "parameters": [{"name": "widgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}}}}
      },
      "delete": {
        "parameters": [{"name": "widgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"204": {}}
      }
    },
    "/gadgets": {
      "post": {
        "requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}}},
        "responses": {"201": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}}}}
      }
    },
    "/gadgets/{gadgetId}": {
      "get": {
        "parameters": [{"name": "gadgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}}}}
      },
      "delete": {
        "parameters": [{"name": "gadgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"204": {}}
      }
    }
  },
  "components": {
    "schemas": {
      "Widget": {"type": "object", "properties": {"name": {"type": "string"}}}
    }
  }
}`

func discoverWithResourceOverrides(t *testing.T, content string, overrides map[string]spec.ResourceOverride) (spec.DiscoveryResult, error) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "spec-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	doc, err := spec.Load("", f.Name(), nil, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return spec.Discover(doc, "test", overrides, nil)
}

func TestResourceTokenOverride_Errors(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]spec.ResourceOverride
		wantErr   string
	}{
		{"missing parts", map[string]spec.ResourceOverride{"Widgets": {Token: "Foo"}}, "must have the form"},
		{"two parts", map[string]spec.ResourceOverride{"Widgets": {Token: "test:Foo"}}, "must have the form"},
		{"empty module", map[string]spec.ResourceOverride{"Widgets": {Token: "test::Foo"}}, "must have the form"},
		{"four parts", map[string]spec.ResourceOverride{"Widgets": {Token: "test:index:a:Foo"}}, "must have the form"},
		{"wrong package", map[string]spec.ResourceOverride{"Widgets": {Token: "other:index:Foo"}}, "provider name"},
		{"wildcard bad token", map[string]spec.ResourceOverride{"*": {Token: "Foo"}}, `resource override "*"`},
		{"same token twice", map[string]spec.ResourceOverride{
			"Widgets": {Token: "test:index:Thing"},
			"Gadgets": {Token: "test:index:Thing"},
		}, "all resolve to token"},
		{"another resource's default token", map[string]spec.ResourceOverride{
			"Widgets": {Token: "test:index:Gadgets"},
		}, "all resolve to token"},
		{"wildcard token on several resources", map[string]spec.ResourceOverride{
			"*": {Token: "test:index:Thing"},
		}, "all resolve to token"},
	}
	for version, content := range map[string]string{"v2": twoResourceSwagger, "v3": twoResourceOAS3} {
		for _, tt := range tests {
			t.Run(version+"/"+tt.name, func(t *testing.T) {
				_, err := discoverWithResourceOverrides(t, content, tt.overrides)
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want it to contain %q", err, tt.wantErr)
				}
			})
		}
	}
}

func TestResourceTokenOverride_Valid(t *testing.T) {
	for version, content := range map[string]string{"v2": twoResourceSwagger, "v3": twoResourceOAS3} {
		t.Run(version, func(t *testing.T) {
			result, err := discoverWithResourceOverrides(t, content, map[string]spec.ResourceOverride{
				"Widgets": {Token: "test:things:Gizmo"},
			})
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			got := map[string]string{}
			for _, r := range result.Resources {
				got[r.Name] = r.Token
			}
			if got["Widgets"] != "test:things:Gizmo" || got["Gadgets"] != "test:index:Gadgets" {
				t.Errorf("tokens = %v", got)
			}
		})
	}
}
