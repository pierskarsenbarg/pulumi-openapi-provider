package runtime

import (
	"context"
	"fmt"

	p "github.com/pulumi/pulumi-go-provider"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/spec"
)

// handleCheck validates that all required inputs are present, returning per-property
// failures for any that are missing. Inputs are passed through unchanged.
func handleCheck(_ context.Context, res spec.ResourceDef, req p.CheckRequest) (p.CheckResponse, error) {
	var failures []p.CheckFailure
	for _, required := range res.RequiredInputs {
		if _, ok := req.Inputs.GetOk(required); !ok {
			failures = append(failures, p.CheckFailure{
				Property: required,
				Reason:   fmt.Sprintf("missing required property %q", required),
			})
		}
	}
	return p.CheckResponse{Inputs: req.Inputs, Failures: failures}, nil
}
