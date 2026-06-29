package openapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/blang/semver"
	p "github.com/pulumi/pulumi-go-provider"
	pschema "github.com/pulumi/pulumi/pkg/v3/codegen/schema"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/config"
	"github.com/pierskarsenbarg/pulumi-openapi-provider/runtime"
	"github.com/pierskarsenbarg/pulumi-openapi-provider/spec"
)

// RunParameterizedProvider starts the openapi-provider binary in parameterized mode.
// The spec URL (and optional --base-url override) are supplied at runtime via the
// Parameterize RPC, allowing users to run:
//
//	pulumi package add openapi-provider 'https://api.example.com/openapi.json'
func RunParameterizedProvider(ctx context.Context, version string) error {
	pp := &parameterizedProvider{binaryVersion: version}
	prov := p.Provider{
		Parameterize: pp.parameterize,
		GetSchema:    pp.getSchema,
		Configure:    pp.configure,
		Check:        pp.check,
		Diff:         pp.diff,
		Create:       pp.create,
		Read:         pp.read,
		Update:       pp.update,
		Delete:       pp.delete,
	}.WithDefaults()
	return p.RunProvider(ctx, "openapi-provider", version, prov)
}

// paramBlob is serialised into ParameterizationSpec.Parameter so the blob is embedded
// in generated SDKs and echoed back in ParameterizeRequestValue.Value on subsequent runs.
type paramBlob struct {
	Spec string `json:"spec"`
	// BaseURL holds the --base-url flag value if it was explicitly supplied; empty otherwise.
	BaseURL string `json:"baseURL,omitempty"`
}

// paramState holds the runtime state populated by a successful Parameterize call.
type paramState struct {
	name    string
	version semver.Version
	blob    []byte
	inner   p.Provider
}

type parameterizedProvider struct {
	binaryVersion string
	mu            sync.RWMutex
	state         *paramState
}

func (pp *parameterizedProvider) parameterize(_ context.Context, req p.ParameterizeRequest) (p.ParameterizeResponse, error) {
	var specSrc, cliBaseURL string

	switch {
	case req.Args != nil:
		pa, err := parseParamArgs(req.Args.Args)
		if err != nil {
			return p.ParameterizeResponse{}, err
		}
		specSrc = pa.spec
		cliBaseURL = pa.baseURL

	case req.Value != nil:
		var blob paramBlob
		if err := json.Unmarshal(req.Value.Value, &blob); err != nil {
			return p.ParameterizeResponse{}, fmt.Errorf("decoding parameterization blob: %w", err)
		}
		specSrc = blob.Spec
		cliBaseURL = blob.BaseURL

	default:
		return p.ParameterizeResponse{}, fmt.Errorf("Parameterize: neither Args nor Value provided")
	}

	doc, err := spec.LoadSpec(specSrc)
	if err != nil {
		return p.ParameterizeResponse{}, fmt.Errorf("loading spec: %w", err)
	}

	// Resolve base URL: CLI flag takes precedence over spec-declared server.
	// An empty result is allowed — the user can supply baseUrl via provider config.
	baseURL := cliBaseURL
	if baseURL == "" {
		baseURL = spec.BaseURL(doc)
	}

	// Derive package name and version from spec metadata.
	var title, specVer string
	info := doc.GetSpecInfo()
	if info.SpecFormat == "oas2" {
		if model, merr := doc.BuildV2Model(); merr == nil && model != nil {
			title = model.Model.Info.Title
			specVer = model.Model.Info.Version
		}
	} else {
		if model, merr := doc.BuildV3Model(); merr == nil && model != nil && model.Model.Info != nil {
			title = model.Model.Info.Title
			specVer = model.Model.Info.Version
		}
	}

	pkgName := slugifyTitle(title)
	pkgVersion, err := semver.Parse(normalizeVersion(specVer))
	if err != nil {
		return p.ParameterizeResponse{}, fmt.Errorf("invalid spec version %q: %w", specVer, err)
	}

	result, err := spec.Discover(doc, pkgName, nil, nil)
	if err != nil {
		return p.ParameterizeResponse{}, fmt.Errorf("discovering resources: %w", err)
	}

	cfg := config.New(nil, baseURL, convertAuthSchemes(result.AuthSchemes), "", nil)
	inner := runtime.Build(pkgName, pkgVersion.String(), result, cfg)

	blob, err := json.Marshal(paramBlob{Spec: specSrc, BaseURL: cliBaseURL})
	if err != nil {
		return p.ParameterizeResponse{}, fmt.Errorf("serialising parameterization blob: %w", err)
	}

	pp.mu.Lock()
	pp.state = &paramState{name: pkgName, version: pkgVersion, blob: blob, inner: inner}
	pp.mu.Unlock()

	return p.ParameterizeResponse{Name: pkgName, Version: pkgVersion}, nil
}

func (pp *parameterizedProvider) getState() (*paramState, error) {
	pp.mu.RLock()
	s := pp.state
	pp.mu.RUnlock()
	if s == nil {
		return nil, fmt.Errorf("provider has not been parameterized")
	}
	return s, nil
}

func (pp *parameterizedProvider) getSchema(_ context.Context, req p.GetSchemaRequest) (p.GetSchemaResponse, error) {
	s, err := pp.getState()
	if err != nil {
		return p.GetSchemaResponse{}, err
	}
	resp, err := s.inner.GetSchema(context.Background(), req)
	if err != nil {
		return resp, err
	}
	// Inject parameterization metadata so the blob gets embedded in generated SDKs.
	// On re-use, Pulumi echoes the blob back via ParameterizeRequestValue.Value.
	var pkg pschema.PackageSpec
	if jsonErr := json.Unmarshal([]byte(resp.Schema), &pkg); jsonErr != nil {
		return resp, nil
	}
	pkg.Parameterization = &pschema.ParameterizationSpec{
		BaseProvider: pschema.BaseProviderSpec{
			Name:    "openapi-provider",
			Version: pp.binaryVersion,
		},
		Parameter: s.blob,
	}
	data, jsonErr := json.Marshal(pkg)
	if jsonErr != nil {
		return resp, nil
	}
	return p.GetSchemaResponse{Schema: string(data)}, nil
}

func (pp *parameterizedProvider) configure(_ context.Context, req p.ConfigureRequest) error {
	s, err := pp.getState()
	if err != nil {
		return err
	}
	return s.inner.Configure(context.Background(), req)
}

func (pp *parameterizedProvider) check(_ context.Context, req p.CheckRequest) (p.CheckResponse, error) {
	s, err := pp.getState()
	if err != nil {
		return p.CheckResponse{}, err
	}
	return s.inner.Check(context.Background(), req)
}

func (pp *parameterizedProvider) diff(_ context.Context, req p.DiffRequest) (p.DiffResponse, error) {
	s, err := pp.getState()
	if err != nil {
		return p.DiffResponse{}, err
	}
	return s.inner.Diff(context.Background(), req)
}

func (pp *parameterizedProvider) create(_ context.Context, req p.CreateRequest) (p.CreateResponse, error) {
	s, err := pp.getState()
	if err != nil {
		return p.CreateResponse{}, err
	}
	return s.inner.Create(context.Background(), req)
}

func (pp *parameterizedProvider) read(_ context.Context, req p.ReadRequest) (p.ReadResponse, error) {
	s, err := pp.getState()
	if err != nil {
		return p.ReadResponse{}, err
	}
	return s.inner.Read(context.Background(), req)
}

func (pp *parameterizedProvider) update(_ context.Context, req p.UpdateRequest) (p.UpdateResponse, error) {
	s, err := pp.getState()
	if err != nil {
		return p.UpdateResponse{}, err
	}
	return s.inner.Update(context.Background(), req)
}

func (pp *parameterizedProvider) delete(_ context.Context, req p.DeleteRequest) error {
	s, err := pp.getState()
	if err != nil {
		return err
	}
	return s.inner.Delete(context.Background(), req)
}

// paramArgs holds the parsed arguments from a Parameterize Args call.
type paramArgs struct {
	spec    string
	baseURL string
}

func parseParamArgs(args []string) (paramArgs, error) {
	if len(args) == 0 {
		return paramArgs{}, fmt.Errorf(
			"usage: pulumi package add openapi-provider <spec-url-or-path> [--base-url=<url>]",
		)
	}
	pa := paramArgs{spec: args[0]}
	for i := 1; i < len(args); i++ {
		arg := args[i]
		switch {
		case strings.HasPrefix(arg, "--base-url="):
			pa.baseURL = strings.TrimPrefix(arg, "--base-url=")
		case arg == "--base-url" && i+1 < len(args):
			i++
			pa.baseURL = args[i]
		}
	}
	return pa, nil
}

// slugifyTitle converts a spec title (e.g. "Petstore API") to a URL-safe package
// name (e.g. "petstore-api"). Non-alphanumeric runs are collapsed to a single hyphen.
func slugifyTitle(title string) string {
	title = strings.ToLower(strings.TrimSpace(title))
	var b strings.Builder
	prevHyphen := true // suppress leading hyphens
	for _, r := range title {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen {
			b.WriteRune('-')
			prevHyphen = true
		}
	}
	result := strings.TrimRight(b.String(), "-")
	if result == "" {
		return "openapi"
	}
	return result
}

// normalizeVersion converts an arbitrary spec version string to semver X.Y.Z form.
// Strips a leading "v" prefix, extracts leading numeric components, and pads to
// three parts. Falls back to "1.0.0" when no numeric content is found.
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")

	rawParts := strings.SplitN(v, ".", 3)
	var nums []string
	for _, part := range rawParts {
		end := 0
		for end < len(part) && part[end] >= '0' && part[end] <= '9' {
			end++
		}
		if end == 0 {
			break
		}
		nums = append(nums, part[:end])
	}

	if len(nums) == 0 {
		return "1.0.0"
	}
	for len(nums) < 3 {
		nums = append(nums, "0")
	}
	return strings.Join(nums[:3], ".")
}
