// Package openapi provides a framework for building Pulumi native providers from OpenAPI specs.
//
// Provider authors import this package, call NewProviderBuilder (or RunProvider for the simple case),
// and get a fully working Pulumi provider that maps OpenAPI resources to Pulumi CRUD operations —
// with no code generation required.
package openapi

import (
	"context"
	"fmt"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi-go-provider/infer"
	"github.com/pulumi/pulumi/sdk/v3/go/common/tokens"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/config"
	"github.com/pierskarsenbarg/pulumi-openapi-provider/runtime"
	"github.com/pierskarsenbarg/pulumi-openapi-provider/spec"
)

// ProviderBuilder builds a Pulumi provider from an OpenAPI spec.
// It mirrors the infer.ProviderBuilder API so engineers can chain standard builder methods
// (WithDescription, WithResources, WithLicense, etc.) before calling Build or Run.
type ProviderBuilder struct {
	name    string
	version string
	inner   *infer.ProviderBuilder
}

// NewProviderBuilder parses the spec and returns a ProviderBuilder pre-wired with the
// OpenAPI dynamic dispatch provider. Engineers can then chain standard builder methods
// before calling Build() or Run().
func NewProviderBuilder(name, version string, opts Options) (*ProviderBuilder, error) {
	dynProvider, err := buildDynamicProvider(name, version, opts)
	if err != nil {
		return nil, err
	}
	inner := infer.NewProviderBuilder().WithWrapped(dynProvider)
	return &ProviderBuilder{name: name, version: version, inner: inner}, nil
}

// Build finalises the provider configuration and returns a p.Provider ready for RunProvider.
// Unlike infer.ProviderBuilder.Build(), this does not require at least one infer resource —
// the OpenAPI-derived resources are provided by the wrapped dynamic dispatch provider.
func (pb *ProviderBuilder) Build() (p.Provider, error) {
	opts := pb.inner.BuildOptions()
	return infer.Provider(opts), nil
}

// Run is a convenience wrapper that calls Build and then p.RunProvider.
func (pb *ProviderBuilder) Run(ctx context.Context) error {
	prov, err := pb.Build()
	if err != nil {
		return err
	}
	return p.RunProvider(ctx, pb.name, pb.version, prov)
}

// WithDescription sets the provider description.
func (pb *ProviderBuilder) WithDescription(desc string) *ProviderBuilder {
	pb.inner = pb.inner.WithDescription(desc)
	return pb
}

// WithDisplayName sets the human-friendly provider display name.
func (pb *ProviderBuilder) WithDisplayName(name string) *ProviderBuilder {
	pb.inner = pb.inner.WithDisplayName(name)
	return pb
}

// WithKeywords adds searchability keywords to the provider metadata.
func (pb *ProviderBuilder) WithKeywords(keywords ...string) *ProviderBuilder {
	pb.inner = pb.inner.WithKeywords(keywords...)
	return pb
}

// WithHomepage sets the provider homepage URL.
func (pb *ProviderBuilder) WithHomepage(url string) *ProviderBuilder {
	pb.inner = pb.inner.WithHomepage(url)
	return pb
}

// WithRepository sets the provider repository URL.
func (pb *ProviderBuilder) WithRepository(url string) *ProviderBuilder {
	pb.inner = pb.inner.WithRepository(url)
	return pb
}

// WithPublisher sets the provider publisher name.
func (pb *ProviderBuilder) WithPublisher(name string) *ProviderBuilder {
	pb.inner = pb.inner.WithPublisher(name)
	return pb
}

// WithLogoURL sets the provider logo URL.
func (pb *ProviderBuilder) WithLogoURL(url string) *ProviderBuilder {
	pb.inner = pb.inner.WithLogoURL(url)
	return pb
}

// WithLicense sets the provider license (e.g. "Apache-2.0").
func (pb *ProviderBuilder) WithLicense(license string) *ProviderBuilder {
	pb.inner = pb.inner.WithLicense(license)
	return pb
}

// WithPluginDownloadURL sets the URL from which the provider plugin binary can be downloaded.
func (pb *ProviderBuilder) WithPluginDownloadURL(url string) *ProviderBuilder {
	pb.inner = pb.inner.WithPluginDownloadURL(url)
	return pb
}

// WithGoImportPath sets the base import path for the generated Go SDK.
func (pb *ProviderBuilder) WithGoImportPath(path string) *ProviderBuilder {
	pb.inner = pb.inner.WithGoImportPath(path)
	return pb
}

// WithNamespace sets the provider namespace.
func (pb *ProviderBuilder) WithNamespace(ns string) *ProviderBuilder {
	pb.inner = pb.inner.WithNamespace(ns)
	return pb
}

// WithLanguageMap sets the language-specific SDK metadata map.
func (pb *ProviderBuilder) WithLanguageMap(m map[string]any) *ProviderBuilder {
	pb.inner = pb.inner.WithLanguageMap(m)
	return pb
}

// WithModuleMap adds a module name mapping.
func (pb *ProviderBuilder) WithModuleMap(m map[tokens.ModuleName]tokens.ModuleName) *ProviderBuilder {
	pb.inner = pb.inner.WithModuleMap(m)
	return pb
}

// WithResources adds hand-crafted infer resources to the provider alongside the OpenAPI-derived ones.
func (pb *ProviderBuilder) WithResources(resources ...infer.InferredResource) *ProviderBuilder {
	pb.inner = pb.inner.WithResources(resources...)
	return pb
}

// WithComponents adds component resources.
func (pb *ProviderBuilder) WithComponents(components ...infer.InferredComponent) *ProviderBuilder {
	pb.inner = pb.inner.WithComponents(components...)
	return pb
}

// WithFunctions adds callable functions.
func (pb *ProviderBuilder) WithFunctions(functions ...infer.InferredFunction) *ProviderBuilder {
	pb.inner = pb.inner.WithFunctions(functions...)
	return pb
}

// RunProvider is a convenience entry point that builds and runs the provider in one call.
// It is equivalent to calling NewProviderBuilder, Build, and p.RunProvider.
func RunProvider(ctx context.Context, name, version string, opts Options) error {
	pb, err := NewProviderBuilder(name, version, opts)
	if err != nil {
		return err
	}
	return pb.Run(ctx)
}

// GetSchema parses the spec and returns the Pulumi schema JSON without starting the provider.
// Useful in CI/CD pipelines to emit schema.json without running the full gRPC server.
func GetSchema(name, version string, opts Options) (string, error) {
	doc, err := spec.Load(opts.SpecURL, opts.SpecPath)
	if err != nil {
		return "", fmt.Errorf("loading spec: %w", err)
	}

	overrides := convertOverrides(opts.Overrides)
	result, err := spec.Discover(doc, name, overrides, opts.ExcludeTags)
	if err != nil {
		return "", fmt.Errorf("discovering resources: %w", err)
	}

	return spec.BuildSchema(name, version, result)
}

// buildDynamicProvider creates the raw p.Provider that handles OpenAPI-derived resources.
func buildDynamicProvider(name, version string, opts Options) (p.Provider, error) {
	doc, err := spec.Load(opts.SpecURL, opts.SpecPath)
	if err != nil {
		return p.Provider{}, fmt.Errorf("loading spec: %w", err)
	}

	overrides := convertOverrides(opts.Overrides)
	result, err := spec.Discover(doc, name, overrides, opts.ExcludeTags)
	if err != nil {
		return p.Provider{}, fmt.Errorf("discovering resources: %w", err)
	}

	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = result.DefaultBaseURL
	}

	cfg := config.New(opts.HTTPClient, baseURL, convertAuthSchemes(result.AuthSchemes))
	return runtime.Build(name, version, result, cfg), nil
}

func convertAuthSchemes(in []spec.AuthScheme) []config.AuthScheme {
	out := make([]config.AuthScheme, len(in))
	for i, s := range in {
		out[i] = config.AuthScheme{
			Kind:       s.Kind,
			ConfigVar:  s.ConfigVar,
			HeaderName: s.HeaderName,
			QueryParam: s.QueryParam,
			Secret:     s.Secret,
		}
	}
	return out
}

func convertOverrides(in map[string]ResourceOverride) map[string]spec.ResourceOverride {
	if in == nil {
		return nil
	}
	out := make(map[string]spec.ResourceOverride, len(in))
	for k, v := range in {
		out[k] = spec.ResourceOverride{
			Skip:         v.Skip,
			Token:        v.Token,
			CreatePath:   v.CreatePath,
			CreateMethod: v.CreateMethod,
			ReadPath:     v.ReadPath,
			UpdatePath:   v.UpdatePath,
			UpdateMethod: v.UpdateMethod,
			DeletePath:   v.DeletePath,
			IDPathParam:  v.IDPathParam,
			IDField:      v.IDField,
		}
	}
	return out
}
