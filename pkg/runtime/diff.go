package runtime

import (
	"context"
	"fmt"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"
)

// computeDiff compares old state with new inputs and returns changed properties.
func computeDiff(_ context.Context, req p.DiffRequest) (p.DiffResponse, error) {
	detailed := map[string]p.PropertyDiff{}

	req.Inputs.All(func(key string, newVal property.Value) bool {
		oldVal, hasOld := req.State.GetOk(key)
		if !hasOld || !valuesEqual(oldVal, newVal) {
			detailed[key] = p.PropertyDiff{Kind: p.Update}
		}
		return true
	})

	req.State.All(func(key string, _ property.Value) bool {
		if _, exists := req.Inputs.GetOk(key); !exists {
			detailed[key] = p.PropertyDiff{Kind: p.Delete}
		}
		return true
	})

	return p.DiffResponse{
		HasChanges:   len(detailed) > 0,
		DetailedDiff: detailed,
	}, nil
}

func valuesEqual(a, b property.Value) bool {
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}
