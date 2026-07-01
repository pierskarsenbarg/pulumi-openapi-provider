// Package config provides runtime configuration for OpenAPI-backed Pulumi providers.
package config

import (
	"encoding/base64"
	"net/http"
	"strings"
	"sync"

	p "github.com/pulumi/pulumi-go-provider"
)

const (
	kindAPIKey = "apiKey"
	kindBearer = "bearer"
)

// AuthScheme describes a single security scheme and how to apply it at runtime.
type AuthScheme struct {
	Kind       string // "apiKey", "bearer", "basic"
	ConfigVar  string // Pulumi config var name holding the credential
	HeaderName string // HTTP header to set (apiKey in header, bearer)
	QueryParam string // query parameter name (apiKey in query; future use)
	Secret     bool
}

// ProviderConfig holds runtime provider configuration including auth and base URL.
type ProviderConfig struct {
	mu                  sync.RWMutex
	BaseURL             string
	authSchemes         []AuthScheme
	schemeValues        map[string]string // configVar → runtime value
	httpClient          *http.Client
	authHeaderOverride  string  // custom header name; empty = use default
	tokenPrefixOverride *string // custom prefix; nil = use default; pointer to allow empty string
	userAgent           string  // "User-Agent" header sent with every request; empty = no header
}

// New creates a ProviderConfig with an optional custom HTTP client, default base URL,
// and the auth schemes discovered from the spec.
// authHeaderOverride and tokenPrefixOverride are optional: pass "" / nil to use defaults.
func New(client *http.Client, defaultBaseURL string, schemes []AuthScheme, authHeaderOverride string, tokenPrefixOverride *string, userAgent string) *ProviderConfig {
	return &ProviderConfig{
		httpClient:          client,
		BaseURL:             defaultBaseURL,
		authSchemes:         schemes,
		schemeValues:        map[string]string{},
		authHeaderOverride:  authHeaderOverride,
		tokenPrefixOverride: tokenPrefixOverride,
		userAgent:           userAgent,
	}
}

// Apply populates config values from a Configure request.
func (c *ProviderConfig) Apply(req p.ConfigureRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := req.Args.GetOk("baseUrl"); ok && v.IsString() {
		if trimmed := strings.TrimSpace(v.AsString()); trimmed != "" {
			c.BaseURL = trimmed
		}
	}

	if len(c.authSchemes) == 0 {
		// fallback: read legacy generic vars
		for _, key := range []string{"apiKey", "apiKeyHeader", "bearerToken"} {
			if v, ok := req.Args.GetOk(key); ok && v.IsString() {
				c.schemeValues[key] = v.AsString()
			}
		}
	} else {
		for _, s := range c.authSchemes {
			switch s.Kind {
			case kindAPIKey, kindBearer:
				if s.ConfigVar != "" {
					if v, ok := req.Args.GetOk(s.ConfigVar); ok && v.IsString() {
						c.schemeValues[s.ConfigVar] = v.AsString()
					}
				}
			case "basic":
				for _, key := range []string{"username", "password"} {
					if v, ok := req.Args.GetOk(key); ok && v.IsString() {
						c.schemeValues[key] = v.AsString()
					}
				}
			}
		}
	}

	if c.BaseURL == "" {
		if v, ok := req.Variables["baseUrl"]; ok && v != "" {
			c.BaseURL = v
		}
	}
}

// AuthHeaders returns HTTP headers derived from the configured auth settings.
func (c *ProviderConfig) AuthHeaders() map[string]string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	headers := map[string]string{}

	if len(c.authSchemes) == 0 {
		// fallback: apply legacy generic vars
		if key := c.schemeValues[kindAPIKey]; key != "" {
			h := c.schemeValues["apiKeyHeader"]
			if h == "" {
				h = "api_key"
			}
			headers[h] = key
		}
		if token := c.schemeValues["bearerToken"]; token != "" {
			headers[c.bearerHeader()] = c.bearerValue(token)
		}
		return headers
	}

	for _, s := range c.authSchemes {
		switch s.Kind {
		case kindAPIKey:
			val := c.schemeValues[s.ConfigVar]
			if val == "" {
				continue
			}
			if s.HeaderName != "" {
				headers[s.HeaderName] = val
			}
			// query param support requires changes in crud.go; skipped for now
		case kindBearer:
			val := c.schemeValues[s.ConfigVar]
			if val != "" {
				headers[c.bearerHeader()] = c.bearerValue(val)
			}
		case "basic":
			user := c.schemeValues["username"]
			pass := c.schemeValues["password"]
			if user != "" {
				creds := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
				headers["Authorization"] = "Basic " + creds
			}
		}
	}
	return headers
}

func (c *ProviderConfig) bearerHeader() string {
	if c.authHeaderOverride != "" {
		return c.authHeaderOverride
	}
	return "Authorization"
}

func (c *ProviderConfig) bearerValue(token string) string {
	prefix := kindBearer
	if c.tokenPrefixOverride != nil {
		prefix = *c.tokenPrefixOverride
	}
	if prefix == "" {
		return token
	}
	return prefix + " " + token
}

// UserAgent returns the "User-Agent" header value to send with requests, or ""
// if none was configured.
func (c *ProviderConfig) UserAgent() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.userAgent
}

// GetBaseURL returns the current base URL.
func (c *ProviderConfig) GetBaseURL() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.BaseURL
}

// Client returns the HTTP client to use for API calls.
func (c *ProviderConfig) Client() *http.Client {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.httpClient != nil {
		return c.httpClient
	}
	return http.DefaultClient
}
