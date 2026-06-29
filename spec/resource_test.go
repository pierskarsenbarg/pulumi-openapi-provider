package spec_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/spec"
)

func TestDiscover_Petstore(t *testing.T) {
	doc, err := spec.Load("https://petstore.swagger.io/v2/swagger.json", "")
	if err != nil {
		t.Skipf("skipping: cannot fetch petstore spec: %v", err)
	}

	result, err := spec.Discover(doc, "petstore", nil, nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(result.Resources) == 0 {
		t.Fatal("expected at least one resource, got none")
	}

	byName := make(map[string]spec.ResourceDef)
	for _, r := range result.Resources {
		byName[r.Name] = r
		t.Logf("discovered resource: %s (token=%s, create=%s, read=%s, delete=%s, id=%s)",
			r.Name, r.Token, r.CreatePath, r.ReadPath, r.DeletePath, r.IDPathParam)
	}

	// Petstore should discover Pet, StoreOrder, User
	for _, name := range []string{"Pet", "StoreOrder", "User"} {
		if _, ok := byName[name]; !ok {
			t.Errorf("expected resource %q to be discovered", name)
		}
	}

	pet := byName["Pet"]
	if pet.CreatePath != "/pet" {
		t.Errorf("Pet.CreatePath = %q, want /pet", pet.CreatePath)
	}
	if pet.ReadPath != "/pet/{petId}" {
		t.Errorf("Pet.ReadPath = %q, want /pet/{petId}", pet.ReadPath)
	}
	if pet.IDPathParam != "petId" {
		t.Errorf("Pet.IDPathParam = %q, want petId", pet.IDPathParam)
	}

	// "id" is reserved by Pulumi and must not appear in input or output schemas.
	for _, name := range []string{"Pet", "StoreOrder"} {
		res := byName[name]
		if _, ok := res.InputSchema["id"]; ok {
			t.Errorf("%s.InputSchema contains reserved property \"id\"", name)
		}
		if _, ok := res.OutputSchema["id"]; ok {
			t.Errorf("%s.OutputSchema contains reserved property \"id\"", name)
		}
	}

	user := byName["User"]
	if user.UpdatePath != "/user/{username}" {
		t.Errorf("User.UpdatePath = %q, want /user/{username}", user.UpdatePath)
	}
	if user.UpdateMethod != "PUT" {
		t.Errorf("User.UpdateMethod = %q, want PUT", user.UpdateMethod)
	}

	if result.DefaultBaseURL == "" {
		t.Error("DefaultBaseURL should not be empty")
	}
	t.Logf("DefaultBaseURL: %s", result.DefaultBaseURL)
}

// minimalSwagger is a self-contained Swagger 2.0 spec with a resource whose
// response schema includes an "id" field. Used to test id-removal without network.
const minimalSwagger = `{
  "swagger": "2.0",
  "info": {"title": "Test", "version": "1.0"},
  "host": "api.example.com",
  "basePath": "/",
  "paths": {
    "/widgets": {
      "post": {
        "operationId": "createWidget",
        "parameters": [{"in": "body", "name": "body", "schema": {"$ref": "#/definitions/Widget"}}],
        "responses": {"201": {"schema": {"$ref": "#/definitions/Widget"}}}
      }
    },
    "/widgets/{widgetId}": {
      "get": {
        "operationId": "getWidget",
        "parameters": [{"in": "path", "name": "widgetId", "required": true, "type": "string"}],
        "responses": {"200": {"schema": {"$ref": "#/definitions/Widget"}}}
      },
      "delete": {
        "operationId": "deleteWidget",
        "parameters": [{"in": "path", "name": "widgetId", "required": true, "type": "string"}],
        "responses": {"204": {}}
      }
    }
  },
  "definitions": {
    "Widget": {
      "type": "object",
      "properties": {
        "id":   {"type": "integer"},
        "name": {"type": "string"}
      }
    }
  }
}`

func TestDiscover_IdNotInSchema(t *testing.T) {
	f, err := os.CreateTemp("", "spec-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(minimalSwagger); err != nil {
		t.Fatal(err)
	}
	f.Close()

	doc, err := spec.Load("", f.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	result, err := spec.Discover(doc, "test", nil, nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(result.Resources) == 0 {
		t.Fatal("expected at least one resource")
	}
	res := result.Resources[0]

	if _, ok := res.InputSchema["id"]; ok {
		t.Errorf("InputSchema contains reserved property \"id\"")
	}
	if _, ok := res.OutputSchema["id"]; ok {
		t.Errorf("OutputSchema contains reserved property \"id\"")
	}
	if _, ok := res.InputSchema["name"]; !ok {
		t.Errorf("InputSchema missing expected property \"name\"")
	}
}

// minimalOAS3 is a self-contained OAS3 spec used for offline tests.
const minimalOAS3 = `{
  "openapi": "3.0.0",
  "info": {"title": "Test", "version": "1.0"},
  "servers": [{"url": "https://api.example.com"}],
  "paths": {
    "/widgets": {
      "post": {
        "requestBody": {
          "content": {
            "application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}
          }
        },
        "responses": {
          "201": {
            "content": {
              "application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}
            }
          }
        }
      }
    },
    "/widgets/{widgetId}": {
      "get": {
        "parameters": [{"name": "widgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {
          "200": {
            "content": {
              "application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}
            }
          }
        }
      },
      "delete": {
        "parameters": [{"name": "widgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"204": {}}
      }
    }
  },
  "components": {
    "schemas": {
      "Widget": {
        "type": "object",
        "properties": {
          "id":   {"type": "integer"},
          "name": {"type": "string"}
        }
      }
    }
  }
}`

func loadInline(t *testing.T, content string) spec.DiscoveryResult {
	t.Helper()
	f, err := os.CreateTemp("", "spec-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	doc, err := spec.Load("", f.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	result, err := spec.Discover(doc, "test", nil, nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	return result
}

func TestDiscover_OAS3_Basic(t *testing.T) {
	result := loadInline(t, minimalOAS3)

	if len(result.Resources) == 0 {
		t.Fatal("expected at least one resource")
	}
	res := result.Resources[0]
	if res.Name != "Widgets" {
		t.Errorf("Name = %q, want Widgets", res.Name)
	}
	if res.CreatePath != "/widgets" {
		t.Errorf("CreatePath = %q, want /widgets", res.CreatePath)
	}
	if res.ReadPath != "/widgets/{widgetId}" {
		t.Errorf("ReadPath = %q, want /widgets/{widgetId}", res.ReadPath)
	}
	if res.IDPathParam != "widgetId" {
		t.Errorf("IDPathParam = %q, want widgetId", res.IDPathParam)
	}
	if result.DefaultBaseURL != "https://api.example.com" {
		t.Errorf("DefaultBaseURL = %q, want https://api.example.com", result.DefaultBaseURL)
	}
}

func TestDiscover_OAS3_IdNotInSchema(t *testing.T) {
	result := loadInline(t, minimalOAS3)
	if len(result.Resources) == 0 {
		t.Fatal("expected at least one resource")
	}
	res := result.Resources[0]
	if _, ok := res.InputSchema["id"]; ok {
		t.Error("InputSchema contains reserved property \"id\"")
	}
	if _, ok := res.OutputSchema["id"]; ok {
		t.Error("OutputSchema contains reserved property \"id\"")
	}
	if _, ok := res.InputSchema["name"]; !ok {
		t.Error("InputSchema missing expected property \"name\"")
	}
}

// V2 auth scheme extraction

const swaggerWithAPIKey = `{
  "swagger": "2.0",
  "info": {"title": "Test", "version": "1.0"},
  "host": "api.example.com",
  "basePath": "/",
  "securityDefinitions": {
    "api_key": {"type": "apiKey", "in": "header", "name": "X-API-Key"}
  },
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
    "Widget": {"type": "object", "properties": {"name": {"type": "string"}}}
  }
}`

const swaggerWithOAuth2 = `{
  "swagger": "2.0",
  "info": {"title": "Test", "version": "1.0"},
  "host": "api.example.com",
  "basePath": "/",
  "securityDefinitions": {
    "petstore_auth": {"type": "oauth2", "flow": "implicit", "authorizationUrl": "https://example.com/oauth/authorize"}
  },
  "paths": {
    "/widgets": {
      "post": {
        "parameters": [{"in": "body", "name": "body", "schema": {"$ref": "#/definitions/Widget"}}],
        "responses": {"201": {"schema": {"$ref": "#/definitions/Widget"}}}
      }
    },
    "/widgets/{widgetId}": {
      "get": {"parameters": [{"in": "path", "name": "widgetId", "required": true, "type": "string"}], "responses": {"200": {"schema": {"$ref": "#/definitions/Widget"}}}},
      "delete": {"parameters": [{"in": "path", "name": "widgetId", "required": true, "type": "string"}], "responses": {"204": {}}}
    }
  },
  "definitions": {
    "Widget": {"type": "object", "properties": {"name": {"type": "string"}}}
  }
}`

const swaggerWithBasic = `{
  "swagger": "2.0",
  "info": {"title": "Test", "version": "1.0"},
  "host": "api.example.com",
  "basePath": "/",
  "securityDefinitions": {
    "basicAuth": {"type": "basic"}
  },
  "paths": {
    "/widgets": {
      "post": {
        "parameters": [{"in": "body", "name": "body", "schema": {"$ref": "#/definitions/Widget"}}],
        "responses": {"201": {"schema": {"$ref": "#/definitions/Widget"}}}
      }
    },
    "/widgets/{widgetId}": {
      "get": {"parameters": [{"in": "path", "name": "widgetId", "required": true, "type": "string"}], "responses": {"200": {"schema": {"$ref": "#/definitions/Widget"}}}},
      "delete": {"parameters": [{"in": "path", "name": "widgetId", "required": true, "type": "string"}], "responses": {"204": {}}}
    }
  },
  "definitions": {
    "Widget": {"type": "object", "properties": {"name": {"type": "string"}}}
  }
}`

func TestAuthSchemes_V2_APIKey(t *testing.T) {
	result := loadInline(t, swaggerWithAPIKey)
	if len(result.AuthSchemes) != 1 {
		t.Fatalf("want 1 auth scheme, got %d", len(result.AuthSchemes))
	}
	s := result.AuthSchemes[0]
	if s.Kind != "apiKey" {
		t.Errorf("Kind = %q, want apiKey", s.Kind)
	}
	if s.HeaderName != "X-API-Key" {
		t.Errorf("HeaderName = %q, want X-API-Key", s.HeaderName)
	}
	if s.ConfigVar != "api_key" {
		t.Errorf("ConfigVar = %q, want api_key", s.ConfigVar)
	}
	if !s.Secret {
		t.Error("expected Secret = true")
	}
}

func TestAuthSchemes_V2_OAuth2(t *testing.T) {
	result := loadInline(t, swaggerWithOAuth2)
	if len(result.AuthSchemes) != 1 {
		t.Fatalf("want 1 auth scheme, got %d", len(result.AuthSchemes))
	}
	s := result.AuthSchemes[0]
	if s.Kind != "bearer" {
		t.Errorf("Kind = %q, want bearer", s.Kind)
	}
	if s.ConfigVar != "bearerToken" {
		t.Errorf("ConfigVar = %q, want bearerToken", s.ConfigVar)
	}
}

func TestAuthSchemes_V2_Basic(t *testing.T) {
	result := loadInline(t, swaggerWithBasic)
	if len(result.AuthSchemes) != 1 {
		t.Fatalf("want 1 auth scheme, got %d", len(result.AuthSchemes))
	}
	if result.AuthSchemes[0].Kind != "basic" {
		t.Errorf("Kind = %q, want basic", result.AuthSchemes[0].Kind)
	}
}

func TestAuthSchemes_V2_None(t *testing.T) {
	result := loadInline(t, minimalSwagger)
	if len(result.AuthSchemes) != 0 {
		t.Errorf("want 0 auth schemes, got %d", len(result.AuthSchemes))
	}
}

// OAS3 auth scheme extraction

const oas3WithBearer = `{
  "openapi": "3.0.0",
  "info": {"title": "Test", "version": "1.0"},
  "servers": [{"url": "https://api.example.com"}],
  "components": {
    "securitySchemes": {
      "BearerAuth": {"type": "http", "scheme": "bearer"}
    },
    "schemas": {
      "Widget": {"type": "object", "properties": {"name": {"type": "string"}}}
    }
  },
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
    }
  }
}`

const oas3WithAPIKey = `{
  "openapi": "3.0.0",
  "info": {"title": "Test", "version": "1.0"},
  "servers": [{"url": "https://api.example.com"}],
  "components": {
    "securitySchemes": {
      "ApiKeyAuth": {"type": "apiKey", "in": "header", "name": "X-API-Key"}
    },
    "schemas": {
      "Widget": {"type": "object", "properties": {"name": {"type": "string"}}}
    }
  },
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
    }
  }
}`

const oas3WithBasic = `{
  "openapi": "3.0.0",
  "info": {"title": "Test", "version": "1.0"},
  "servers": [{"url": "https://api.example.com"}],
  "components": {
    "securitySchemes": {
      "BasicAuth": {"type": "http", "scheme": "basic"}
    },
    "schemas": {
      "Widget": {"type": "object", "properties": {"name": {"type": "string"}}}
    }
  },
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
    }
  }
}`

func TestAuthSchemes_OAS3_Bearer(t *testing.T) {
	result := loadInline(t, oas3WithBearer)
	if len(result.AuthSchemes) != 1 {
		t.Fatalf("want 1 auth scheme, got %d", len(result.AuthSchemes))
	}
	s := result.AuthSchemes[0]
	if s.Kind != "bearer" {
		t.Errorf("Kind = %q, want bearer", s.Kind)
	}
	if s.ConfigVar != "bearerToken" {
		t.Errorf("ConfigVar = %q, want bearerToken", s.ConfigVar)
	}
	if s.HeaderName != "Authorization" {
		t.Errorf("HeaderName = %q, want Authorization", s.HeaderName)
	}
}

func TestAuthSchemes_OAS3_APIKey(t *testing.T) {
	result := loadInline(t, oas3WithAPIKey)
	if len(result.AuthSchemes) != 1 {
		t.Fatalf("want 1 auth scheme, got %d", len(result.AuthSchemes))
	}
	s := result.AuthSchemes[0]
	if s.Kind != "apiKey" {
		t.Errorf("Kind = %q, want apiKey", s.Kind)
	}
	if s.HeaderName != "X-API-Key" {
		t.Errorf("HeaderName = %q, want X-API-Key", s.HeaderName)
	}
	if s.ConfigVar != "apiKeyAuth" {
		t.Errorf("ConfigVar = %q, want apiKeyAuth", s.ConfigVar)
	}
}

func TestAuthSchemes_OAS3_Basic(t *testing.T) {
	result := loadInline(t, oas3WithBasic)
	if len(result.AuthSchemes) != 1 {
		t.Fatalf("want 1 auth scheme, got %d", len(result.AuthSchemes))
	}
	if result.AuthSchemes[0].Kind != "basic" {
		t.Errorf("Kind = %q, want basic", result.AuthSchemes[0].Kind)
	}
}

func TestAuthSchemes_OAS3_None(t *testing.T) {
	result := loadInline(t, minimalOAS3)
	if len(result.AuthSchemes) != 0 {
		t.Errorf("want 0 auth schemes, got %d", len(result.AuthSchemes))
	}
}

// Override application

func TestDiscover_Override_Skip(t *testing.T) {
	f, err := os.CreateTemp("", "spec-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(minimalSwagger)
	f.Close()

	doc, err := spec.Load("", f.Name())
	if err != nil {
		t.Fatal(err)
	}
	result, err := spec.Discover(doc, "test", map[string]spec.ResourceOverride{
		"Widget": {Skip: true},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range result.Resources {
		if r.Name == "Widget" {
			t.Error("Widget should have been skipped")
		}
	}
}

func TestDiscover_Override_Token(t *testing.T) {
	f, err := os.CreateTemp("", "spec-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(minimalSwagger)
	f.Close()

	doc, err := spec.Load("", f.Name())
	if err != nil {
		t.Fatal(err)
	}
	result, err := spec.Discover(doc, "test", map[string]spec.ResourceOverride{
		"Widgets": {Token: "test:index:Gadget"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Resources) == 0 {
		t.Fatal("expected a resource")
	}
	if result.Resources[0].Token != "test:index:Gadget" {
		t.Errorf("Token = %q, want test:index:Gadget", result.Resources[0].Token)
	}
}

// swaggerWithTags is a Swagger 2.0 spec with two tagged resources: "widgets" (tag "widgets")
// and "gadgets" (tag "gadgets"). Used to test ExcludeTags.
const swaggerWithTags = `{
  "swagger": "2.0",
  "info": {"title": "Test", "version": "1.0"},
  "host": "api.example.com",
  "basePath": "/",
  "tags": [
    {"name": "widgets"},
    {"name": "gadgets"}
  ],
  "paths": {
    "/widgets": {
      "post": {
        "tags": ["widgets"],
        "parameters": [{"in": "body", "name": "body", "schema": {"$ref": "#/definitions/Widget"}}],
        "responses": {"201": {"schema": {"$ref": "#/definitions/Widget"}}}
      }
    },
    "/widgets/{widgetId}": {
      "get": {
        "tags": ["widgets"],
        "parameters": [{"in": "path", "name": "widgetId", "required": true, "type": "string"}],
        "responses": {"200": {"schema": {"$ref": "#/definitions/Widget"}}}
      },
      "delete": {
        "tags": ["widgets"],
        "parameters": [{"in": "path", "name": "widgetId", "required": true, "type": "string"}],
        "responses": {"204": {}}
      }
    },
    "/gadgets": {
      "post": {
        "tags": ["gadgets"],
        "parameters": [{"in": "body", "name": "body", "schema": {"$ref": "#/definitions/Gadget"}}],
        "responses": {"201": {"schema": {"$ref": "#/definitions/Gadget"}}}
      }
    },
    "/gadgets/{gadgetId}": {
      "get": {
        "tags": ["gadgets"],
        "parameters": [{"in": "path", "name": "gadgetId", "required": true, "type": "string"}],
        "responses": {"200": {"schema": {"$ref": "#/definitions/Gadget"}}}
      },
      "delete": {
        "tags": ["gadgets"],
        "parameters": [{"in": "path", "name": "gadgetId", "required": true, "type": "string"}],
        "responses": {"204": {}}
      }
    }
  },
  "definitions": {
    "Widget": {"type": "object", "properties": {"name": {"type": "string"}}},
    "Gadget": {"type": "object", "properties": {"name": {"type": "string"}}}
  }
}`

// oas3WithTags is an OAS3 spec equivalent to swaggerWithTags.
const oas3WithTags = `{
  "openapi": "3.0.0",
  "info": {"title": "Test", "version": "1.0"},
  "servers": [{"url": "https://api.example.com"}],
  "tags": [
    {"name": "widgets"},
    {"name": "gadgets"}
  ],
  "paths": {
    "/widgets": {
      "post": {
        "tags": ["widgets"],
        "requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}}},
        "responses": {"201": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}}}}
      }
    },
    "/widgets/{widgetId}": {
      "get": {
        "tags": ["widgets"],
        "parameters": [{"name": "widgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}}}}
      },
      "delete": {
        "tags": ["widgets"],
        "parameters": [{"name": "widgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"204": {}}
      }
    },
    "/gadgets": {
      "post": {
        "tags": ["gadgets"],
        "requestBody": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Gadget"}}}},
        "responses": {"201": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Gadget"}}}}}
      }
    },
    "/gadgets/{gadgetId}": {
      "get": {
        "tags": ["gadgets"],
        "parameters": [{"name": "gadgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"200": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Gadget"}}}}}
      },
      "delete": {
        "tags": ["gadgets"],
        "parameters": [{"name": "gadgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"204": {}}
      }
    }
  },
  "components": {
    "schemas": {
      "Widget": {"type": "object", "properties": {"name": {"type": "string"}}},
      "Gadget": {"type": "object", "properties": {"name": {"type": "string"}}}
    }
  }
}`

func loadInlineWithTags(t *testing.T, content string, excludeTags []string) spec.DiscoveryResult {
	t.Helper()
	f, err := os.CreateTemp("", "spec-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	doc, err := spec.Load("", f.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	result, err := spec.Discover(doc, "test", nil, excludeTags)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	return result
}

func TestDiscover_ExcludeTags_V2_SingleTag(t *testing.T) {
	result := loadInlineWithTags(t, swaggerWithTags, []string{"widgets"})
	for _, r := range result.Resources {
		if r.Name == "Widgets" {
			t.Error("Widgets should have been excluded by tag")
		}
	}
	found := false
	for _, r := range result.Resources {
		if r.Name == "Gadgets" {
			found = true
		}
	}
	if !found {
		t.Error("Gadgets should still be discovered when only 'widgets' tag is excluded")
	}
}

func TestDiscover_ExcludeTags_V2_MultipleTag(t *testing.T) {
	result := loadInlineWithTags(t, swaggerWithTags, []string{"widgets", "gadgets"})
	if len(result.Resources) != 0 {
		t.Errorf("expected no resources when all tags are excluded, got %d", len(result.Resources))
	}
}

func TestDiscover_ExcludeTags_V2_NoMatch(t *testing.T) {
	result := loadInlineWithTags(t, swaggerWithTags, []string{"unknown-tag"})
	if len(result.Resources) != 2 {
		t.Errorf("expected 2 resources when excluded tag doesn't match, got %d", len(result.Resources))
	}
}

func TestDiscover_ExcludeTags_OAS3_SingleTag(t *testing.T) {
	result := loadInlineWithTags(t, oas3WithTags, []string{"widgets"})
	for _, r := range result.Resources {
		if r.Name == "Widgets" {
			t.Error("Widgets should have been excluded by tag")
		}
	}
	found := false
	for _, r := range result.Resources {
		if r.Name == "Gadgets" {
			found = true
		}
	}
	if !found {
		t.Error("Gadgets should still be discovered when only 'widgets' tag is excluded")
	}
}

func TestDiscover_ExcludeTags_OAS3_MultipleTag(t *testing.T) {
	result := loadInlineWithTags(t, oas3WithTags, []string{"widgets", "gadgets"})
	if len(result.Resources) != 0 {
		t.Errorf("expected no resources when all tags are excluded, got %d", len(result.Resources))
	}
}

func TestDiscover_ExcludeTags_OAS3_NoMatch(t *testing.T) {
	result := loadInlineWithTags(t, oas3WithTags, []string{"unknown-tag"})
	if len(result.Resources) != 2 {
		t.Errorf("expected 2 resources when excluded tag doesn't match, got %d", len(result.Resources))
	}
}

func TestBuildSchema_Petstore(t *testing.T) {
	doc, err := spec.Load("https://petstore.swagger.io/v2/swagger.json", "")
	if err != nil {
		t.Skipf("skipping: cannot fetch petstore spec: %v", err)
	}

	result, err := spec.Discover(doc, "petstore", nil, nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	schema, err := spec.BuildSchema("petstore", "0.1.0", result)
	if err != nil {
		t.Fatalf("BuildSchema: %v", err)
	}

	if len(schema) == 0 {
		t.Fatal("schema is empty")
	}
	t.Logf("schema length: %d bytes", len(schema))
	t.Logf("schema preview: %.200s...", schema)
}

// --- Base URL derivation ---

const swaggerNoHost = `{
  "swagger": "2.0",
  "info": {"title": "Test", "version": "1.0"},
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
    "Widget": {"type": "object", "properties": {"name": {"type": "string"}}}
  }
}`

const swaggerHTTPOnly = `{
  "swagger": "2.0",
  "info": {"title": "Test", "version": "1.0"},
  "host": "api.example.com",
  "schemes": ["http"],
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
    "Widget": {"type": "object", "properties": {"name": {"type": "string"}}}
  }
}`

const swaggerHTTPAndHTTPS = `{
  "swagger": "2.0",
  "info": {"title": "Test", "version": "1.0"},
  "host": "api.example.com",
  "schemes": ["http", "https"],
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
    "Widget": {"type": "object", "properties": {"name": {"type": "string"}}}
  }
}`

const swaggerWithBasePath = `{
  "swagger": "2.0",
  "info": {"title": "Test", "version": "1.0"},
  "host": "api.example.com",
  "basePath": "/v2",
  "schemes": ["https"],
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
    "Widget": {"type": "object", "properties": {"name": {"type": "string"}}}
  }
}`

const oas3NoServers = `{
  "openapi": "3.0.0",
  "info": {"title": "Test", "version": "1.0"},
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
    }
  },
  "components": {
    "schemas": {
      "Widget": {"type": "object", "properties": {"name": {"type": "string"}}}
    }
  }
}`

func TestBaseURL_V2_NoHost_ReturnsEmpty(t *testing.T) {
	result := loadInline(t, swaggerNoHost)
	if result.DefaultBaseURL != "" {
		t.Errorf("DefaultBaseURL = %q, want empty string when spec has no host", result.DefaultBaseURL)
	}
}

func TestBaseURL_V2_HTTPOnly(t *testing.T) {
	result := loadInline(t, swaggerHTTPOnly)
	if result.DefaultBaseURL != "http://api.example.com" {
		t.Errorf("DefaultBaseURL = %q, want http://api.example.com", result.DefaultBaseURL)
	}
}

func TestBaseURL_V2_HTTPSPreferredOverHTTP(t *testing.T) {
	result := loadInline(t, swaggerHTTPAndHTTPS)
	if result.DefaultBaseURL != "https://api.example.com" {
		t.Errorf("DefaultBaseURL = %q, want https://api.example.com", result.DefaultBaseURL)
	}
}

func TestBaseURL_V2_SchemesCaseInsensitive(t *testing.T) {
	spec := strings.ReplaceAll(swaggerHTTPOnly, `"schemes": ["http"]`, `"schemes": ["HTTP"]`)
	result := loadInline(t, spec)
	if result.DefaultBaseURL != "http://api.example.com" {
		t.Errorf("DefaultBaseURL = %q, want http://api.example.com for uppercase scheme", result.DefaultBaseURL)
	}
}

func TestBaseURL_V2_NonHTTPSchemeIgnored(t *testing.T) {
	// "ws" is valid in Swagger 2.0 but not usable by the HTTP client; should default to https.
	spec := strings.ReplaceAll(swaggerHTTPOnly, `"schemes": ["http"]`, `"schemes": ["ws"]`)
	result := loadInline(t, spec)
	if result.DefaultBaseURL != "https://api.example.com" {
		t.Errorf("DefaultBaseURL = %q, want https://api.example.com when only non-HTTP schemes present", result.DefaultBaseURL)
	}
}

func TestBaseURL_V2_HostWithBasePath(t *testing.T) {
	result := loadInline(t, swaggerWithBasePath)
	if result.DefaultBaseURL != "https://api.example.com/v2" {
		t.Errorf("DefaultBaseURL = %q, want https://api.example.com/v2", result.DefaultBaseURL)
	}
}

func TestBaseURL_V3_UsesFirstServerURL(t *testing.T) {
	result := loadInline(t, minimalOAS3)
	if result.DefaultBaseURL != "https://api.example.com" {
		t.Errorf("DefaultBaseURL = %q, want https://api.example.com", result.DefaultBaseURL)
	}
}

func TestBaseURL_V3_NoServers_ReturnsEmpty(t *testing.T) {
	result := loadInline(t, oas3NoServers)
	if result.DefaultBaseURL != "" {
		t.Errorf("DefaultBaseURL = %q, want empty string when spec has no servers", result.DefaultBaseURL)
	}
}

// --- Enum support ---

const swaggerInlineEnum = `{
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
      "required": ["name", "status"],
      "properties": {
        "name":   {"type": "string"},
        "status": {"type": "string", "enum": ["active", "inactive", "pending"]}
      }
    }
  }
}`

func TestEnum_V2_InlineStringEnum(t *testing.T) {
	result := loadInline(t, swaggerInlineEnum)
	if len(result.Resources) == 0 {
		t.Fatal("expected at least one resource")
	}
	res := result.Resources[0]

	// The inline enum on Widget.status should have been registered as a named type.
	const wantToken = "test:index:WidgetsStatus"
	enumType, ok := result.Types[wantToken]
	if !ok {
		t.Fatalf("Types map missing expected enum token %q; got %v", wantToken, result.Types)
	}
	if enumType.Type != "string" {
		t.Errorf("enum type = %q, want string", enumType.Type)
	}
	if len(enumType.Enum) != 3 {
		t.Fatalf("enum values count = %d, want 3", len(enumType.Enum))
	}
	wantValues := []string{"active", "inactive", "pending"}
	for i, v := range enumType.Enum {
		if v.Value != wantValues[i] {
			t.Errorf("enum[%d].Value = %v, want %q", i, v.Value, wantValues[i])
		}
	}

	// The property on the resource should reference the enum type, not use a plain "string".
	prop, ok := res.InputSchema["status"]
	if !ok {
		t.Fatal("InputSchema missing property \"status\"")
	}
	wantRef := "#/types/test:index:WidgetsStatus"
	if prop.TypeSpec.Ref != wantRef {
		t.Errorf("status TypeSpec.Ref = %q, want %q", prop.TypeSpec.Ref, wantRef)
	}
	if prop.TypeSpec.Type != "" {
		t.Errorf("status TypeSpec.Type = %q, want empty (should use Ref)", prop.TypeSpec.Type)
	}
}

const swaggerNamedEnum = `{
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
    "WidgetStatus": {
      "type": "string",
      "enum": ["active", "inactive", "pending"]
    },
    "Widget": {
      "type": "object",
      "required": ["name", "status"],
      "properties": {
        "name":   {"type": "string"},
        "status": {"$ref": "#/definitions/WidgetStatus"}
      }
    }
  }
}`

func TestEnum_V2_NamedEnum(t *testing.T) {
	result := loadInline(t, swaggerNamedEnum)
	if len(result.Resources) == 0 {
		t.Fatal("expected at least one resource")
	}
	res := result.Resources[0]

	// The named WidgetStatus definition should be registered as an enum type.
	const wantToken = "test:index:WidgetStatus"
	enumType, ok := result.Types[wantToken]
	if !ok {
		t.Fatalf("Types map missing expected enum token %q; got %v", wantToken, result.Types)
	}
	if enumType.Type != "string" {
		t.Errorf("enum type = %q, want string", enumType.Type)
	}
	if len(enumType.Enum) != 3 {
		t.Fatalf("enum values count = %d, want 3", len(enumType.Enum))
	}

	// The status property should reference the named enum type.
	prop, ok := res.InputSchema["status"]
	if !ok {
		t.Fatal("InputSchema missing property \"status\"")
	}
	wantRef := "#/types/test:index:WidgetStatus"
	if prop.TypeSpec.Ref != wantRef {
		t.Errorf("status TypeSpec.Ref = %q, want %q", prop.TypeSpec.Ref, wantRef)
	}
}

const oas3InlineEnum = `{
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
    }
  },
  "components": {
    "schemas": {
      "Widget": {
        "type": "object",
        "required": ["name", "status"],
        "properties": {
          "name":   {"type": "string"},
          "status": {"type": "string", "enum": ["active", "inactive", "pending"]}
        }
      }
    }
  }
}`

func TestEnum_V3_InlineStringEnum(t *testing.T) {
	result := loadInline(t, oas3InlineEnum)
	if len(result.Resources) == 0 {
		t.Fatal("expected at least one resource")
	}
	res := result.Resources[0]

	// Widget.status is an inline enum — should be registered under the resource name + property name.
	const wantToken = "test:index:WidgetsStatus"
	enumType, ok := result.Types[wantToken]
	if !ok {
		t.Fatalf("Types map missing expected enum token %q; got %v", wantToken, result.Types)
	}
	if enumType.Type != "string" {
		t.Errorf("enum type = %q, want string", enumType.Type)
	}
	if len(enumType.Enum) != 3 {
		t.Fatalf("enum values count = %d, want 3", len(enumType.Enum))
	}
	wantValues := []string{"active", "inactive", "pending"}
	for i, v := range enumType.Enum {
		if v.Value != wantValues[i] {
			t.Errorf("enum[%d].Value = %v, want %q", i, v.Value, wantValues[i])
		}
	}

	prop, ok := res.InputSchema["status"]
	if !ok {
		t.Fatal("InputSchema missing property \"status\"")
	}
	wantRef := "#/types/test:index:WidgetsStatus"
	if prop.TypeSpec.Ref != wantRef {
		t.Errorf("status TypeSpec.Ref = %q, want %q", prop.TypeSpec.Ref, wantRef)
	}
	if prop.TypeSpec.Type != "" {
		t.Errorf("status TypeSpec.Type = %q, want empty (should use Ref)", prop.TypeSpec.Type)
	}
}

const oas3NamedEnum = `{
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
    }
  },
  "components": {
    "schemas": {
      "WidgetStatus": {
        "type": "string",
        "enum": ["active", "inactive", "pending"]
      },
      "Widget": {
        "type": "object",
        "required": ["name", "status"],
        "properties": {
          "name":   {"type": "string"},
          "status": {"$ref": "#/components/schemas/WidgetStatus"}
        }
      }
    }
  }
}`

func TestEnum_V3_NamedEnum(t *testing.T) {
	result := loadInline(t, oas3NamedEnum)
	if len(result.Resources) == 0 {
		t.Fatal("expected at least one resource")
	}
	res := result.Resources[0]

	// The named WidgetStatus component should be registered as an enum type.
	const wantToken = "test:index:WidgetStatus"
	enumType, ok := result.Types[wantToken]
	if !ok {
		t.Fatalf("Types map missing expected enum token %q; got %v", wantToken, result.Types)
	}
	if enumType.Type != "string" {
		t.Errorf("enum type = %q, want string", enumType.Type)
	}
	if len(enumType.Enum) != 3 {
		t.Fatalf("enum values count = %d, want 3", len(enumType.Enum))
	}

	// The status property should reference the named enum type.
	prop, ok := res.InputSchema["status"]
	if !ok {
		t.Fatal("InputSchema missing property \"status\"")
	}
	wantRef := "#/types/test:index:WidgetStatus"
	if prop.TypeSpec.Ref != wantRef {
		t.Errorf("status TypeSpec.Ref = %q, want %q", prop.TypeSpec.Ref, wantRef)
	}
}

// ---------------------------------------------------------------------------
// Deprecated operation tests
// ---------------------------------------------------------------------------

const swaggerDeprecated = `{
  "swagger": "2.0",
  "info": {"title": "Test", "version": "1.0"},
  "host": "api.example.com",
  "basePath": "/",
  "paths": {
    "/widgets": {
      "post": {
        "deprecated": true,
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
    },
    "/gadgets": {
      "post": {
        "parameters": [{"in": "body", "name": "body", "schema": {"$ref": "#/definitions/Widget"}}],
        "responses": {"201": {"schema": {"$ref": "#/definitions/Widget"}}}
      }
    },
    "/gadgets/{gadgetId}": {
      "get": {
        "parameters": [{"in": "path", "name": "gadgetId", "required": true, "type": "string"}],
        "responses": {"200": {"schema": {"$ref": "#/definitions/Widget"}}}
      },
      "delete": {
        "parameters": [{"in": "path", "name": "gadgetId", "required": true, "type": "string"}],
        "responses": {"204": {}}
      }
    }
  },
  "definitions": {
    "Widget": {"type": "object", "properties": {"name": {"type": "string"}}}
  }
}`

func TestDiscover_V2_DeprecatedResource(t *testing.T) {
	result := loadInline(t, swaggerDeprecated)

	byName := map[string]spec.ResourceDef{}
	for _, r := range result.Resources {
		byName[r.Name] = r
	}

	widgets, ok := byName["Widgets"]
	if !ok {
		t.Fatal("expected Widgets resource to be discovered")
	}
	if !widgets.Deprecated {
		t.Error("Widgets.Deprecated = false, want true (create op is deprecated)")
	}

	gadgets, ok := byName["Gadgets"]
	if !ok {
		t.Fatal("expected Gadgets resource to be discovered")
	}
	if gadgets.Deprecated {
		t.Error("Gadgets.Deprecated = true, want false (create op is not deprecated)")
	}
}

const oas3Deprecated = `{
  "openapi": "3.0.0",
  "info": {"title": "Test", "version": "1.0"},
  "servers": [{"url": "https://api.example.com"}],
  "paths": {
    "/widgets": {
      "post": {
        "deprecated": true,
        "requestBody": {
          "content": {
            "application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}
          }
        },
        "responses": {
          "201": {
            "content": {
              "application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}
            }
          }
        }
      }
    },
    "/widgets/{widgetId}": {
      "get": {
        "parameters": [{"name": "widgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {
          "200": {
            "content": {
              "application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}
            }
          }
        }
      },
      "delete": {
        "parameters": [{"name": "widgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"204": {}}
      }
    },
    "/gadgets": {
      "post": {
        "requestBody": {
          "content": {
            "application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}
          }
        },
        "responses": {
          "201": {
            "content": {
              "application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}
            }
          }
        }
      }
    },
    "/gadgets/{gadgetId}": {
      "get": {
        "parameters": [{"name": "gadgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {
          "200": {
            "content": {
              "application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}
            }
          }
        }
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

func TestDiscover_V3_DeprecatedResource(t *testing.T) {
	result := loadInline(t, oas3Deprecated)

	byName := map[string]spec.ResourceDef{}
	for _, r := range result.Resources {
		byName[r.Name] = r
	}

	widgets, ok := byName["Widgets"]
	if !ok {
		t.Fatal("expected Widgets resource to be discovered")
	}
	if !widgets.Deprecated {
		t.Error("Widgets.Deprecated = false, want true (create op is deprecated)")
	}

	gadgets, ok := byName["Gadgets"]
	if !ok {
		t.Fatal("expected Gadgets resource to be discovered")
	}
	if gadgets.Deprecated {
		t.Error("Gadgets.Deprecated = true, want false (create op is not deprecated)")
	}
}

func TestBuildSchema_DeprecatedResource(t *testing.T) {
	result := loadInline(t, swaggerDeprecated)

	schema, err := spec.BuildSchema("test", "0.1.0", result)
	if err != nil {
		t.Fatalf("BuildSchema: %v", err)
	}

	// Widgets was created with deprecated:true — its schema entry must carry deprecationMessage.
	if !strings.Contains(schema, `"deprecationMessage"`) {
		t.Error("schema JSON missing deprecationMessage for deprecated resource")
	}

	// Gadgets was not deprecated — its token must not carry deprecationMessage.
	// Parse just enough to find the Gadgets resource block.
	var parsed struct {
		Resources map[string]struct {
			DeprecationMessage string `json:"deprecationMessage"`
		} `json:"resources"`
	}
	if err := json.Unmarshal([]byte(schema), &parsed); err != nil {
		t.Fatalf("unmarshal schema: %v", err)
	}

	var widgetsToken, gadgetsToken string
	for tok := range parsed.Resources {
		if strings.HasSuffix(tok, ":Widgets") {
			widgetsToken = tok
		}
		if strings.HasSuffix(tok, ":Gadgets") {
			gadgetsToken = tok
		}
	}

	if widgetsToken == "" {
		t.Fatal("Widgets resource not found in schema")
	}
	if gadgetsToken == "" {
		t.Fatal("Gadgets resource not found in schema")
	}

	if parsed.Resources[widgetsToken].DeprecationMessage == "" {
		t.Errorf("Widgets resource has no deprecationMessage in schema")
	}
	if parsed.Resources[gadgetsToken].DeprecationMessage != "" {
		t.Errorf("Gadgets resource has unexpected deprecationMessage %q", parsed.Resources[gadgetsToken].DeprecationMessage)
	}
}

// ---------------------------------------------------------------------------
// Trailing-slash path tests (APIs like NetBox where all paths end in "/")
// ---------------------------------------------------------------------------

const oas3TrailingSlash = `{
  "openapi": "3.0.0",
  "info": {"title": "Test", "version": "1.0"},
  "servers": [{"url": "https://api.example.com"}],
  "paths": {
    "/widgets/": {
      "post": {
        "requestBody": {
          "content": {
            "application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}
          }
        },
        "responses": {
          "201": {
            "content": {
              "application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}
            }
          }
        }
      }
    },
    "/widgets/{widgetId}/": {
      "get": {
        "parameters": [{"name": "widgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {
          "200": {
            "content": {
              "application/json": {"schema": {"$ref": "#/components/schemas/Widget"}}
            }
          }
        }
      },
      "delete": {
        "parameters": [{"name": "widgetId", "in": "path", "required": true, "schema": {"type": "string"}}],
        "responses": {"204": {}}
      }
    }
  },
  "components": {
    "schemas": {
      "Widget": {
        "type": "object",
        "properties": {
          "id":   {"type": "integer"},
          "name": {"type": "string"}
        }
      }
    }
  }
}`

func TestDiscover_TrailingSlashPaths(t *testing.T) {
	result := loadInline(t, oas3TrailingSlash)

	if len(result.Resources) == 0 {
		t.Fatal("expected at least one resource; trailing-slash paths were not matched")
	}

	res := result.Resources[0]
	if res.Name != "Widgets" {
		t.Errorf("Name = %q, want Widgets", res.Name)
	}
	if _, ok := res.InputSchema["name"]; !ok {
		t.Error("InputSchema missing expected property \"name\"")
	}
}

func TestDiscover_NetBox(t *testing.T) {
	doc, err := spec.Load("http://localhost:8000/api/schema/", "")
	if err != nil {
		t.Skipf("skipping: cannot fetch NetBox spec: %v", err)
	}

	result, err := spec.Discover(doc, "netbox", nil, nil)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	if len(result.Resources) == 0 {
		t.Fatal("expected resources from NetBox spec, got none — trailing-slash path lookup may be broken")
	}
	t.Logf("discovered %d resources from NetBox", len(result.Resources))
}
