package runtime

import (
	"context"
	"fmt"
	"strings"

	p "github.com/pulumi/pulumi-go-provider"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/config"
	"github.com/pierskarsenbarg/pulumi-openapi-provider/spec"
)

// Build constructs a p.Provider that dispatches CRUD calls to the appropriate HTTP endpoints
// based on the discovered resource definitions.
func Build(pkgName, version string, result spec.DiscoveryResult, cfg *config.ProviderConfig) p.Provider {
	byToken := make(map[string]spec.ResourceDef, len(result.Resources))
	for _, res := range result.Resources {
		byToken[res.Token] = res
	}

	schemaJSON, _ := spec.BuildSchema(pkgName, version, result)

	return p.Provider{
		GetSchema: func(_ context.Context, _ p.GetSchemaRequest) (p.GetSchemaResponse, error) {
			return p.GetSchemaResponse{Schema: schemaJSON}, nil
		},

		Configure: func(_ context.Context, req p.ConfigureRequest) error {
			cfg.Apply(req)
			return nil
		},

		Check: func(ctx context.Context, req p.CheckRequest) (p.CheckResponse, error) {
			res, err := lookupResource(byToken, string(req.Urn))
			if err != nil {
				return p.CheckResponse{}, err
			}
			return handleCheck(ctx, res, req)
		},

		Create: func(ctx context.Context, req p.CreateRequest) (p.CreateResponse, error) {
			res, err := lookupResource(byToken, string(req.Urn))
			if err != nil {
				return p.CreateResponse{}, err
			}
			return handleCreate(ctx, res, req, cfg)
		},

		Read: func(ctx context.Context, req p.ReadRequest) (p.ReadResponse, error) {
			res, err := lookupResource(byToken, string(req.Urn))
			if err != nil {
				return p.ReadResponse{}, err
			}
			return handleRead(ctx, res, req, cfg)
		},

		Update: func(ctx context.Context, req p.UpdateRequest) (p.UpdateResponse, error) {
			res, err := lookupResource(byToken, string(req.Urn))
			if err != nil {
				return p.UpdateResponse{}, err
			}
			return handleUpdate(ctx, res, req, cfg)
		},

		Delete: func(ctx context.Context, req p.DeleteRequest) error {
			res, err := lookupResource(byToken, string(req.Urn))
			if err != nil {
				return err
			}
			return handleDelete(ctx, res, req, cfg)
		},

		Diff: func(ctx context.Context, req p.DiffRequest) (p.DiffResponse, error) {
			return computeDiff(ctx, req)
		},
	}.WithDefaults()
}

// lookupResource extracts the resource type token from a URN and looks it up in the dispatch table.
// URN format: urn:pulumi:stack::project::pkg:module:Type::name
func lookupResource(byToken map[string]spec.ResourceDef, urn string) (spec.ResourceDef, error) {
	token := tokenFromURN(urn)
	res, ok := byToken[token]
	if !ok {
		return spec.ResourceDef{}, fmt.Errorf("unknown resource type %q", token)
	}
	return res, nil
}

// tokenFromURN extracts the type token from a Pulumi URN.
// e.g. "urn:pulumi:stack::project::mypkg:index:Pet::myPet" → "mypkg:index:Pet"
func tokenFromURN(urn string) string {
	parts := strings.Split(urn, "::")
	if len(parts) < 3 {
		return urn
	}
	return parts[2]
}
