package config_test

import (
	"encoding/base64"
	"net/http"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/config"
)

func req(args map[string]string) p.ConfigureRequest {
	m := map[string]property.Value{}
	for k, v := range args {
		m[k] = property.New(v)
	}
	return p.ConfigureRequest{Args: property.NewMap(m)}
}

func TestNew_DefaultBaseURL(t *testing.T) {
	cfg := config.New(nil, "https://api.example.com", nil, "", nil)
	if cfg.GetBaseURL() != "https://api.example.com" {
		t.Errorf("BaseURL = %q, want https://api.example.com", cfg.GetBaseURL())
	}
}

func TestNew_CustomHTTPClient(t *testing.T) {
	client := &http.Client{}
	cfg := config.New(client, "", nil, "", nil)
	if cfg.Client() != client {
		t.Error("Client() did not return the supplied HTTP client")
	}
}

func TestNew_DefaultHTTPClient(t *testing.T) {
	cfg := config.New(nil, "", nil, "", nil)
	if cfg.Client() != http.DefaultClient {
		t.Error("Client() should return http.DefaultClient when none supplied")
	}
}

func TestApply_BaseURL(t *testing.T) {
	cfg := config.New(nil, "https://default.example.com", nil, "", nil)
	cfg.Apply(req(map[string]string{"baseUrl": "https://override.example.com"}))
	if cfg.GetBaseURL() != "https://override.example.com" {
		t.Errorf("BaseURL = %q, want https://override.example.com", cfg.GetBaseURL())
	}
}

func TestApply_BaseURL_NotOverriddenByEmpty(t *testing.T) {
	cfg := config.New(nil, "https://default.example.com", nil, "", nil)
	cfg.Apply(req(map[string]string{"baseUrl": ""}))
	if cfg.GetBaseURL() != "https://default.example.com" {
		t.Errorf("BaseURL = %q, want default to be preserved", cfg.GetBaseURL())
	}
}

func TestApply_BaseURL_WhitespaceIsIgnored(t *testing.T) {
	cfg := config.New(nil, "https://default.example.com", nil, "", nil)
	cfg.Apply(req(map[string]string{"baseUrl": "   "}))
	if cfg.GetBaseURL() != "https://default.example.com" {
		t.Errorf("BaseURL = %q, want default to be preserved when config value is whitespace-only", cfg.GetBaseURL())
	}
}

func TestApply_BaseURL_Trimmed(t *testing.T) {
	cfg := config.New(nil, "", nil, "", nil)
	cfg.Apply(req(map[string]string{"baseUrl": "  https://api.example.com  "}))
	if cfg.GetBaseURL() != "https://api.example.com" {
		t.Errorf("BaseURL = %q, want leading/trailing whitespace stripped", cfg.GetBaseURL())
	}
}

func TestApply_BaseURL_FromVariables(t *testing.T) {
	cfg := config.New(nil, "", nil, "", nil)
	cfg.Apply(p.ConfigureRequest{
		Args:      property.NewMap(map[string]property.Value{}),
		Variables: map[string]string{"baseUrl": "https://via-vars.example.com"},
	})
	if cfg.GetBaseURL() != "https://via-vars.example.com" {
		t.Errorf("BaseURL = %q, want https://via-vars.example.com", cfg.GetBaseURL())
	}
}

// --- apiKey scheme ---

func TestApply_APIKey_Header(t *testing.T) {
	schemes := []config.AuthScheme{{Kind: "apiKey", ConfigVar: "myApiKey", HeaderName: "X-API-Key", Secret: true}}
	cfg := config.New(nil, "", schemes, "", nil)
	cfg.Apply(req(map[string]string{"myApiKey": "secret123"}))

	headers := cfg.AuthHeaders()
	if headers["X-API-Key"] != "secret123" {
		t.Errorf("X-API-Key = %q, want secret123", headers["X-API-Key"])
	}
}

func TestAuthHeaders_APIKey_EmptyValue(t *testing.T) {
	schemes := []config.AuthScheme{{Kind: "apiKey", ConfigVar: "myApiKey", HeaderName: "X-API-Key"}}
	cfg := config.New(nil, "", schemes, "", nil)
	// no Apply call — value is empty
	if _, ok := cfg.AuthHeaders()["X-API-Key"]; ok {
		t.Error("expected no X-API-Key header when value is empty")
	}
}

// --- bearer scheme ---

func TestApply_Bearer(t *testing.T) {
	schemes := []config.AuthScheme{{Kind: "bearer", ConfigVar: "bearerToken", HeaderName: "Authorization", Secret: true}}
	cfg := config.New(nil, "", schemes, "", nil)
	cfg.Apply(req(map[string]string{"bearerToken": "tok123"}))

	headers := cfg.AuthHeaders()
	if headers["Authorization"] != "bearer tok123" {
		t.Errorf("Authorization = %q, want bearer tok123", headers["Authorization"])
	}
}

func TestAuthHeaders_Bearer_EmptyValue(t *testing.T) {
	schemes := []config.AuthScheme{{Kind: "bearer", ConfigVar: "bearerToken", HeaderName: "Authorization"}}
	cfg := config.New(nil, "", schemes, "", nil)
	if _, ok := cfg.AuthHeaders()["Authorization"]; ok {
		t.Error("expected no Authorization header when bearer token is empty")
	}
}

// --- basic scheme ---

func TestApply_Basic(t *testing.T) {
	schemes := []config.AuthScheme{{Kind: "basic"}}
	cfg := config.New(nil, "", schemes, "", nil)
	cfg.Apply(req(map[string]string{"username": "alice", "password": "s3cr3t"}))

	headers := cfg.AuthHeaders()
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("alice:s3cr3t"))
	if headers["Authorization"] != expected {
		t.Errorf("Authorization = %q, want %q", headers["Authorization"], expected)
	}
}

func TestAuthHeaders_Basic_NoUsername(t *testing.T) {
	schemes := []config.AuthScheme{{Kind: "basic"}}
	cfg := config.New(nil, "", schemes, "", nil)
	if _, ok := cfg.AuthHeaders()["Authorization"]; ok {
		t.Error("expected no Authorization header when username is empty")
	}
}

// --- fallback (no schemes) ---

func TestApply_Fallback_APIKey(t *testing.T) {
	cfg := config.New(nil, "", nil, "", nil)
	cfg.Apply(req(map[string]string{"apiKey": "k", "apiKeyHeader": "X-Token"}))

	headers := cfg.AuthHeaders()
	if headers["X-Token"] != "k" {
		t.Errorf("X-Token = %q, want k", headers["X-Token"])
	}
}

func TestApply_Fallback_APIKey_DefaultHeader(t *testing.T) {
	cfg := config.New(nil, "", nil, "", nil)
	cfg.Apply(req(map[string]string{"apiKey": "k"}))

	headers := cfg.AuthHeaders()
	if headers["api_key"] != "k" {
		t.Errorf("api_key = %q, want k", headers["api_key"])
	}
}

func TestApply_Fallback_BearerToken(t *testing.T) {
	cfg := config.New(nil, "", nil, "", nil)
	cfg.Apply(req(map[string]string{"bearerToken": "tok"}))

	headers := cfg.AuthHeaders()
	if headers["Authorization"] != "bearer tok" {
		t.Errorf("Authorization = %q, want bearer tok", headers["Authorization"])
	}
}

func TestAuthHeaders_Fallback_Empty(t *testing.T) {
	cfg := config.New(nil, "", nil, "", nil)
	if len(cfg.AuthHeaders()) != 0 {
		t.Errorf("expected no headers when no auth configured, got %v", cfg.AuthHeaders())
	}
}

// --- AuthOverride ---

func TestAuthOverride_CustomHeader(t *testing.T) {
	schemes := []config.AuthScheme{{Kind: "bearer", ConfigVar: "bearerToken"}}
	cfg := config.New(nil, "", schemes, "X-Auth-Token", nil)
	cfg.Apply(req(map[string]string{"bearerToken": "tok"}))

	headers := cfg.AuthHeaders()
	if _, ok := headers["Authorization"]; ok {
		t.Error("expected no Authorization header when header override is set")
	}
	if headers["X-Auth-Token"] != "bearer tok" {
		t.Errorf("X-Auth-Token = %q, want bearer tok", headers["X-Auth-Token"])
	}
}

func TestAuthOverride_CustomPrefix(t *testing.T) {
	schemes := []config.AuthScheme{{Kind: "bearer", ConfigVar: "bearerToken"}}
	prefix := "token"
	cfg := config.New(nil, "", schemes, "", &prefix)
	cfg.Apply(req(map[string]string{"bearerToken": "tok"}))

	headers := cfg.AuthHeaders()
	if headers["Authorization"] != "token tok" {
		t.Errorf("Authorization = %q, want token tok", headers["Authorization"])
	}
}

func TestAuthOverride_EmptyPrefix(t *testing.T) {
	schemes := []config.AuthScheme{{Kind: "bearer", ConfigVar: "bearerToken"}}
	prefix := ""
	cfg := config.New(nil, "", schemes, "", &prefix)
	cfg.Apply(req(map[string]string{"bearerToken": "tok"}))

	headers := cfg.AuthHeaders()
	if headers["Authorization"] != "tok" {
		t.Errorf("Authorization = %q, want raw token with no prefix", headers["Authorization"])
	}
}

func TestAuthOverride_CustomHeaderAndPrefix(t *testing.T) {
	schemes := []config.AuthScheme{{Kind: "bearer", ConfigVar: "bearerToken"}}
	prefix := "token"
	cfg := config.New(nil, "", schemes, "X-Auth-Token", &prefix)
	cfg.Apply(req(map[string]string{"bearerToken": "tok"}))

	headers := cfg.AuthHeaders()
	if headers["X-Auth-Token"] != "token tok" {
		t.Errorf("X-Auth-Token = %q, want token tok", headers["X-Auth-Token"])
	}
}

func TestAuthOverride_Fallback_CustomHeader(t *testing.T) {
	// AuthOverride also applies to the legacy fallback bearerToken path.
	cfg := config.New(nil, "", nil, "X-Auth-Token", nil)
	cfg.Apply(req(map[string]string{"bearerToken": "tok"}))

	headers := cfg.AuthHeaders()
	if _, ok := headers["Authorization"]; ok {
		t.Error("expected no Authorization header in fallback path when header override is set")
	}
	if headers["X-Auth-Token"] != "bearer tok" {
		t.Errorf("X-Auth-Token = %q, want bearer tok", headers["X-Auth-Token"])
	}
}
