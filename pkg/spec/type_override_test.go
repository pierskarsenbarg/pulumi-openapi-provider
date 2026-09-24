package spec_test

import (
	"os"
	"strings"
	"testing"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/pkg/spec"
)

func discoverWithTypeOverrides(t *testing.T, content string, overrides map[string]spec.TypeOverride) (spec.DiscoveryResult, error) {
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
	return spec.Discover(doc, "test", nil, overrides, nil)
}

// typeOverrideFixtures returns V2 and V3 specs where Widget references the named enum
// WidgetStatus both directly (status) and as an array item (statuses).
func typeOverrideFixtures() map[string]string {
	v2 := strings.Replace(swaggerNamedEnum,
		`"name":   {"type": "string"},`,
		`"name":   {"type": "string"},
        "statuses": {"type": "array", "items": {"$ref": "#/definitions/WidgetStatus"}},`, 1)
	v3 := strings.Replace(oas3NamedEnum,
		`"name":   {"type": "string"},`,
		`"name":   {"type": "string"},
          "statuses": {"type": "array", "items": {"$ref": "#/components/schemas/WidgetStatus"}},`, 1)
	return map[string]string{"v2": v2, "v3": v3}
}

func TestTypeOverride_RenamesTokenAndRefs(t *testing.T) {
	for name, content := range typeOverrideFixtures() {
		t.Run(name, func(t *testing.T) {
			result, err := discoverWithTypeOverrides(t, content, map[string]spec.TypeOverride{
				"WidgetStatus": {Token: "test:index:State"},
			})
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			if _, ok := result.Types["test:index:State"]; !ok {
				t.Errorf("Types missing renamed token; got %v", result.Types)
			}
			if _, ok := result.Types["test:index:WidgetStatus"]; ok {
				t.Error("Types still contains the default token")
			}

			res := result.Resources[0]
			const wantRef = "#/types/test:index:State"
			if got := res.InputSchema["status"].Ref; got != wantRef {
				t.Errorf("status Ref = %q, want %q", got, wantRef)
			}
			items := res.InputSchema["statuses"].Items
			if items == nil || items.Ref != wantRef {
				t.Errorf("statuses items = %+v, want Ref %q", items, wantRef)
			}
		})
	}
}

func TestTypeOverride_RenamesObjectType(t *testing.T) {
	const v2 = `{
  "swagger": "2.0",
  "info": {"title": "Test", "version": "1.0"},
  "host": "api.example.com",
  "basePath": "/",
  "paths": {
    "/widgets": {
      "post": {
        "parameters": [{"in": "body", "name": "body", "schema": {"$ref": "#/definitions/Widget"}}],
        "responses": {"201": {"schema": {"$ref": "#/definitions/Widget"}}}
      }
    },
    "/widgets/{widgetId}": {
      "get": {
        "parameters": [{"in": "path", "name": "widgetId", "required": true, "type": "string"}],
        "responses": {"200": {"schema": {"$ref": "#/definitions/Widget"}}}
      },
      "delete": {
        "parameters": [{"in": "path", "name": "widgetId", "required": true, "type": "string"}],
        "responses": {"204": {}}
      }
    }
  },
  "definitions": {
    "Widget": {
      "type": "object",
      "properties": {
        "name": {"type": "string"},
        "owner": {"$ref": "#/definitions/Owner"}
      }
    },
    "Owner": {
      "type": "object",
      "properties": {"email": {"type": "string"}}
    }
  }
}`
	result, err := discoverWithTypeOverrides(t, v2, map[string]spec.TypeOverride{
		"Owner": {Token: "test:people:Person"},
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if _, ok := result.Types["test:people:Person"]; !ok {
		t.Errorf("Types missing renamed token; got %v", result.Types)
	}
	if got, want := result.Resources[0].InputSchema["owner"].Ref, "#/types/test:people:Person"; got != want {
		t.Errorf("owner Ref = %q, want %q", got, want)
	}
}

func TestTypeOverride_Errors(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]spec.TypeOverride
		wantErr   string
	}{
		{"missing parts", map[string]spec.TypeOverride{"WidgetStatus": {Token: "Foo"}}, "must have the form"},
		{"two parts", map[string]spec.TypeOverride{"WidgetStatus": {Token: "test:Foo"}}, "must have the form"},
		{"empty module", map[string]spec.TypeOverride{"WidgetStatus": {Token: "test::Foo"}}, "must have the form"},
		{"four parts", map[string]spec.TypeOverride{"WidgetStatus": {Token: "test:index:a:Foo"}}, "must have the form"},
		{"wrong package", map[string]spec.TypeOverride{"WidgetStatus": {Token: "other:index:Foo"}}, "provider name"},
		{"unknown key", map[string]spec.TypeOverride{"Nope": {Token: "test:index:Foo"}}, "no such definition"},
		{"same token twice", map[string]spec.TypeOverride{
			"WidgetStatus": {Token: "test:index:Foo"},
			"Widget":       {Token: "test:index:Foo"},
		}, "also used by"},
		{"another schema's default token", map[string]spec.TypeOverride{
			"WidgetStatus": {Token: "test:index:Widget"},
		}, "also used by"},
	}
	for version, content := range typeOverrideFixtures() {
		for _, tt := range tests {
			t.Run(version+"/"+tt.name, func(t *testing.T) {
				_, err := discoverWithTypeOverrides(t, content, tt.overrides)
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
