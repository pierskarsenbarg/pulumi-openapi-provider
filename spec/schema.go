package spec

import (
	"encoding/json"
	"fmt"

	pschema "github.com/pulumi/pulumi/pkg/v3/codegen/schema"
)

// BuildSchema generates a Pulumi PackageSpec JSON string from discovered resources.
func BuildSchema(pkgName, version string, result DiscoveryResult) (string, error) {
	resources := map[string]pschema.ResourceSpec{}
	for _, res := range result.Resources {
		resources[res.Token] = resourceSpec(res)
	}

	language, err := packageLanguage(pkgName)
	if err != nil {
		return "", fmt.Errorf("marshaling package language metadata: %w", err)
	}

	pkg := pschema.PackageSpec{
		Name:      pkgName,
		Version:   version,
		Language:  language,
		Resources: resources,
		Types:     result.Types,
		Config:    providerConfigSpec(result.AuthSchemes),
		Provider:  providerSpec(result.AuthSchemes),
	}

	data, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshaling schema: %w", err)
	}
	return string(data), nil
}

func packageLanguage(pkgName string) (map[string]pschema.RawMessage, error) {
	language := map[string]any{
		"csharp": map[string]any{
			"respectSchemaVersion": true,
		},
		"go": map[string]any{
			"generateResourceContainerTypes": true,
			"importBasePath":                 fmt.Sprintf("local-package/sdk/go/%s", pkgName),
			"respectSchemaVersion":           true,
		},
		"nodejs": map[string]any{
			"respectSchemaVersion": true,
		},
		"python": map[string]any{
			"pyproject": map[string]any{
				"enabled": true,
			},
			"respectSchemaVersion": true,
		},
	}

	out := make(map[string]pschema.RawMessage, len(language))
	for name, info := range language {
		data, err := json.Marshal(info)
		if err != nil {
			return nil, err
		}
		out[name] = pschema.RawMessage(data)
	}
	return out, nil
}

func resourceSpec(res ResourceDef) pschema.ResourceSpec {
	// Outputs include all input properties plus any read-only properties.
	allOutputProps := map[string]pschema.PropertySpec{}
	for k, v := range res.InputSchema {
		allOutputProps[k] = v
	}
	for k, v := range res.OutputSchema {
		if _, alreadyInput := allOutputProps[k]; !alreadyInput {
			allOutputProps[k] = v
		}
	}

	return pschema.ResourceSpec{
		ObjectTypeSpec: pschema.ObjectTypeSpec{
			Properties: allOutputProps,
		},
		InputProperties: res.InputSchema,
		RequiredInputs:  res.RequiredInputs,
	}
}

func providerConfigSpec(schemes []AuthScheme) pschema.ConfigSpec {
	return pschema.ConfigSpec{Variables: authConfigVars(schemes)}
}

func providerSpec(schemes []AuthScheme) pschema.ResourceSpec {
	return pschema.ResourceSpec{InputProperties: authConfigVars(schemes)}
}

// authConfigVars returns the Pulumi config variable map derived from the discovered security
// schemes. If no schemes were found the spec has no security declarations, so we fall back to
// generic variables that let provider authors configure auth manually.
func authConfigVars(schemes []AuthScheme) map[string]pschema.PropertySpec {
	vars := map[string]pschema.PropertySpec{
		"baseUrl": {
			TypeSpec:    pschema.TypeSpec{Type: "string"},
			Description: "Base URL for the API (overrides the spec server URL).",
		},
	}

	if len(schemes) == 0 {
		vars["apiKey"] = pschema.PropertySpec{
			TypeSpec:    pschema.TypeSpec{Type: "string"},
			Description: "API key value.",
			Secret:      true,
		}
		vars["apiKeyHeader"] = pschema.PropertySpec{
			TypeSpec:    pschema.TypeSpec{Type: "string"},
			Description: "HTTP header name for the API key (default: api_key).",
		}
		vars["bearerToken"] = pschema.PropertySpec{
			TypeSpec:    pschema.TypeSpec{Type: "string"},
			Description: "Bearer token for the Authorization header.",
			Secret:      true,
		}
		return vars
	}

	seen := map[string]bool{}
	for _, s := range schemes {
		switch s.Kind {
		case "apiKey", "bearer":
			if s.ConfigVar != "" && !seen[s.ConfigVar] {
				seen[s.ConfigVar] = true
				desc := s.Description
				if desc == "" && s.Kind == "bearer" {
					desc = "Bearer token for the Authorization header."
				} else if desc == "" {
					desc = "API key credential."
				}
				vars[s.ConfigVar] = pschema.PropertySpec{
					TypeSpec:    pschema.TypeSpec{Type: "string"},
					Description: desc,
					Secret:      true,
				}
			}
		case "basic":
			if !seen["username"] {
				seen["username"] = true
				vars["username"] = pschema.PropertySpec{
					TypeSpec:    pschema.TypeSpec{Type: "string"},
					Description: "Username for HTTP Basic authentication.",
				}
			}
			if !seen["password"] {
				seen["password"] = true
				vars["password"] = pschema.PropertySpec{
					TypeSpec:    pschema.TypeSpec{Type: "string"},
					Description: "Password for HTTP Basic authentication.",
					Secret:      true,
				}
			}
		}
	}
	return vars
}
