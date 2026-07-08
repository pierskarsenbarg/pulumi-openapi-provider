package config

import "fmt"

// ResolveUserAgent returns the explicit override if set, otherwise the default
// "pulumi-openapi-provider/{version}" string.
func ResolveUserAgent(override, version string) string {
	if override != "" {
		return override
	}
	return fmt.Sprintf("pulumi-openapi-provider/%s", version)
}
