package openapi

import (
	"context"
	"net/http"
	"time"

	p "github.com/pulumi/pulumi-go-provider"
)

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
	// The special key "*" applies to every resource as a baseline; resource-specific
	// entries take precedence over it on a field-by-field basis.
	Overrides map[string]ResourceOverride
	// ExcludeTags lists OpenAPI operation tags whose associated resources should be
	// excluded from discovery. Any resource whose CRUD operations include at least
	// one matching tag is skipped entirely.
	ExcludeTags []string
	// AuthOverride overrides how the bearer token is sent when the API does not follow
	// standard conventions. If nil, defaults apply ("Authorization" header, "bearer" prefix).
	// Only available in library (code) mode; ignored by the parameterized provider.
	AuthOverride *AuthOverride
	// DisablePolling skips the post-create and post-delete readiness checks.
	// By default the provider polls the read endpoint after create (until the resource
	// exists) and after delete (until the resource is gone).
	DisablePolling bool
	// PollingOptions tunes the backoff and timeout used during polling.
	// Zero values use the defaults: 5 min timeout, 1 s initial interval, 30 s max, 1.5× multiplier.
	PollingOptions PollingOptions
}

// PollingOptions controls how the provider waits for resources to reach a stable state
// after create and delete operations.
type PollingOptions struct {
	// Timeout is the maximum time to wait before declaring the operation failed.
	// Default: 5 minutes.
	Timeout time.Duration
	// InitialInterval is the delay before the first poll.
	// Default: 1 second.
	InitialInterval time.Duration
	// MaxInterval caps the delay between polls as the backoff grows.
	// Default: 30 seconds.
	MaxInterval time.Duration
	// Multiplier is the factor by which the interval grows after each poll.
	// Default: 1.5.
	Multiplier float64
}

// AuthOverride lets provider authors override the HTTP header name and/or token prefix
// used when sending credentials to APIs that deviate from standard conventions.
// The credential value itself is always supplied by the Pulumi end-user at runtime.
type AuthOverride struct {
	// HeaderName is the HTTP header to use instead of "Authorization".
	// Leave empty to keep the default.
	HeaderName string
	// TokenPrefix is the string prepended to the token value (e.g. "token" produces
	// "token <value>"). Set to "" to send the raw token with no prefix.
	TokenPrefix string
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
	// Check replaces the built-in input validation for this resource.
	// When nil, the default required-field check is used.
	Check func(ctx context.Context, req p.CheckRequest) (p.CheckResponse, error)
	// Diff replaces the built-in diff computation for this resource.
	// When nil, property-level change detection is used.
	Diff func(ctx context.Context, req p.DiffRequest) (p.DiffResponse, error)
	// Create replaces the built-in HTTP POST create logic for this resource.
	// When nil, the spec-derived create endpoint is called.
	Create func(ctx context.Context, req p.CreateRequest) (p.CreateResponse, error)
	// Read replaces the built-in HTTP GET read logic for this resource.
	// When nil, the spec-derived read endpoint is called.
	Read func(ctx context.Context, req p.ReadRequest) (p.ReadResponse, error)
	// Update replaces the built-in HTTP PUT/PATCH update logic for this resource.
	// When nil, the spec-derived update endpoint is called.
	Update func(ctx context.Context, req p.UpdateRequest) (p.UpdateResponse, error)
	// Delete replaces the built-in HTTP DELETE logic for this resource.
	// When nil, the spec-derived delete endpoint is called.
	Delete func(ctx context.Context, req p.DeleteRequest) error
}
