package runtime

import (
	"context"

	p "github.com/pulumi/pulumi-go-provider"
)

// ResourceHooks holds optional function overrides for a single resource's CRUD operations.
// Any nil field falls back to the built-in auto-generated behaviour.
type ResourceHooks struct {
	Check  func(ctx context.Context, req p.CheckRequest) (p.CheckResponse, error)
	Diff   func(ctx context.Context, req p.DiffRequest) (p.DiffResponse, error)
	Create func(ctx context.Context, req p.CreateRequest) (p.CreateResponse, error)
	Read   func(ctx context.Context, req p.ReadRequest) (p.ReadResponse, error)
	Update func(ctx context.Context, req p.UpdateRequest) (p.UpdateResponse, error)
	Delete func(ctx context.Context, req p.DeleteRequest) error
}
