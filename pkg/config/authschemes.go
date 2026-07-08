package config

import "github.com/pierskarsenbarg/pulumi-openapi-provider/pkg/spec"

// AuthSchemesFromSpec converts the auth schemes discovered from an OpenAPI spec into the
// runtime AuthScheme type used by ProviderConfig.
func AuthSchemesFromSpec(in []spec.AuthScheme) []AuthScheme {
	out := make([]AuthScheme, len(in))
	for i, s := range in {
		out[i] = AuthScheme{
			Kind:       s.Kind,
			ConfigVar:  s.ConfigVar,
			HeaderName: s.HeaderName,
			QueryParam: s.QueryParam,
			Secret:     s.Secret,
		}
	}
	return out
}
