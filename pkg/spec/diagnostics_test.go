package spec_test

import (
	"strings"
	"testing"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/pkg/spec"
)

func TestResourceOverride_UnmatchedKeyErrors(t *testing.T) {
	for version, content := range map[string]string{"v2": twoResourceSwagger, "v3": twoResourceOAS3} {
		t.Run(version, func(t *testing.T) {
			_, err := discoverWithResourceOverrides(t, content, map[string]spec.ResourceOverride{
				"Nope": {IDField: "id"},
			})
			if err == nil || !strings.Contains(err.Error(), `resource override "Nope" matches no discovered resource`) {
				t.Errorf("error = %v, want unmatched key error", err)
			}
		})
	}
}

func TestResourceOverride_MatchedKeysAndWildcardOK(t *testing.T) {
	for version, content := range map[string]string{"v2": twoResourceSwagger, "v3": twoResourceOAS3} {
		t.Run(version, func(t *testing.T) {
			result, err := discoverWithResourceOverrides(t, content, map[string]spec.ResourceOverride{
				"*":       {IDField: "id"},
				"Widgets": {Skip: true},
			})
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			if len(result.Resources) != 1 || result.Resources[0].Name != "Gadgets" {
				t.Errorf("resources = %+v, want only Gadgets", result.Resources)
			}
		})
	}
}

const nearMissSwagger = `{
  "swagger": "2.0",
  "info": {"title": "Test", "version": "1.0"},
  "host": "api.example.com",
  "basePath": "/",
  "paths": {
    "/gadgets": {
      "post": {"parameters": [{"in": "body", "name": "body", "schema": {"type": "object"}}],
               "responses": {"201": {"schema": {"type": "object"}}}}
    },
    "/gadgets/{gadgetId}": {
      "put": {"parameters": [{"in": "path", "name": "gadgetId", "required": true, "type": "string"}],
              "responses": {"200": {"schema": {"type": "object"}}}}
    },
    "/things": {
      "get": {"responses": {"200": {"schema": {"type": "object"}}}}
    },
    "/things/{thingId}": {
      "get": {"parameters": [{"in": "path", "name": "thingId", "required": true, "type": "string"}],
              "responses": {"200": {"schema": {"type": "object"}}}}
    }
  }
}`

const nearMissOAS3 = `{
  "openapi": "3.0.0",
  "info": {"title": "Test", "version": "1.0"},
  "servers": [{"url": "https://api.example.com"}],
  "paths": {
    "/gadgets": {
      "post": {
        "requestBody": {"content": {"application/json": {"schema": {"type": "object"}}}},
        "responses": {"201": {"content": {"application/json": {"schema": {"type": "object"}}}}}
      }
    },
    "/gadgets/{gadgetId}": {
      "put": {
        "parameters": [{"name": "gadgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"content": {"application/json": {"schema": {"type": "object"}}}}}
      }
    },
    "/things": {
      "get": {"responses": {"200": {"content": {"application/json": {"schema": {"type": "object"}}}}}}
    },
    "/things/{thingId}": {
      "get": {
        "parameters": [{"name": "thingId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"content": {"application/json": {"schema": {"type": "object"}}}}}
      }
    }
  }
}`

func TestDiscover_WarnsOnCreateWithoutReadOrDelete(t *testing.T) {
	for version, content := range map[string]string{"v2": nearMissSwagger, "v3": nearMissOAS3} {
		t.Run(version, func(t *testing.T) {
			result, err := discoverWithResourceOverrides(t, content, nil)
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			if len(result.Resources) != 0 {
				t.Errorf("resources = %+v, want none", result.Resources)
			}
			if len(result.Warnings) != 1 {
				t.Fatalf("warnings = %v, want exactly one (Gadgets); Things has no create op and must stay silent", result.Warnings)
			}
			if w := result.Warnings[0]; !strings.Contains(w, `"Gadgets"`) || !strings.Contains(w, "/gadgets/{gadgetId}") {
				t.Errorf("warning = %q, want it to name Gadgets and its item path", w)
			}
		})
	}
}

func TestDiscover_NoWarningsForManageableResources(t *testing.T) {
	for version, content := range map[string]string{"v2": twoResourceSwagger, "v3": twoResourceOAS3} {
		t.Run(version, func(t *testing.T) {
			result, err := discoverWithResourceOverrides(t, content, nil)
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			if len(result.Warnings) != 0 {
				t.Errorf("warnings = %v, want none", result.Warnings)
			}
		})
	}
}
