package openapi

import "net/http"

// Options configures an OpenAPI-based Pulumi provider.
type Options struct {
	// SpecURL is the URL to fetch the OpenAPI/Swagger spec from.
	SpecURL string
	// SpecPath is the local file path to the OpenAPI/Swagger spec.
	SpecPath string
	// BaseURL overrides the server URL extracted from the spec.
	BaseURL string
	// HTTPClient is an optional custom HTTP client for API calls.
	HTTPClient *http.Client
	// Overrides customizes how discovered resources map to Pulumi operations.
	// Keys are the discovered resource names (e.g. "Pet", "StoreOrder").
	Overrides map[string]ResourceOverride
	// ExcludeTags lists OpenAPI operation tags whose associated resources should be
	// excluded from discovery. Any resource whose CRUD operations include at least
	// one matching tag is skipped entirely.
	ExcludeTags []string
}

// ResourceOverride customizes how a discovered resource maps to Pulumi operations.
// Fields left empty inherit the convention-based value.
type ResourceOverride struct {
	// Skip excludes this resource from discovery entirely.
	Skip bool
	// Token overrides the generated resource token (e.g. "mypkg:index:Widget").
	Token string
	// CreatePath overrides the discovered create endpoint path.
	CreatePath string
	// CreateMethod overrides the HTTP method for create (default: POST).
	CreateMethod string
	// ReadPath overrides the discovered read endpoint path.
	ReadPath string
	// UpdatePath overrides the discovered update endpoint path.
	UpdatePath string
	// UpdateMethod overrides the HTTP method for update (e.g. "PATCH").
	UpdateMethod string
	// DeletePath overrides the discovered delete endpoint path.
	DeletePath string
	// IDPathParam overrides the path parameter name used as the resource ID.
	IDPathParam string
	// IDField overrides the JSON response field used to extract the resource ID.
	// Defaults to the IDPathParam name.
	IDField string
}
