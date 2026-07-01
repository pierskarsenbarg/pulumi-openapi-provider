package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strings"
	"time"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/config"
	"github.com/pierskarsenbarg/pulumi-openapi-provider/spec"
)

const (
	defaultPollingTimeout         = 5 * time.Minute
	defaultPollingInitialInterval = 1 * time.Second
	defaultPollingMaxInterval     = 30 * time.Second
	defaultPollingMultiplier      = 1.5
)

// PollingConfig holds resolved (defaults applied) polling parameters.
type PollingConfig struct {
	Timeout         time.Duration
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
}

// ResolvePollingConfig fills zero-value fields with defaults.
func ResolvePollingConfig(timeout, initial, max time.Duration, multiplier float64) PollingConfig {
	if timeout == 0 {
		timeout = defaultPollingTimeout
	}
	if initial == 0 {
		initial = defaultPollingInitialInterval
	}
	if max == 0 {
		max = defaultPollingMaxInterval
	}
	if multiplier == 0 {
		multiplier = defaultPollingMultiplier
	}
	return PollingConfig{
		Timeout:         timeout,
		InitialInterval: initial,
		MaxInterval:     max,
		Multiplier:      multiplier,
	}
}

type crudClient struct {
	cfg            *config.ProviderConfig
	pollingEnabled bool
	polling        PollingConfig
}

// create calls POST on the create endpoint and returns the new resource ID and state.
func (c *crudClient) create(ctx context.Context, res spec.ResourceDef, inputs property.Map) (string, property.Map, error) {
	body := propertyMapToAPIBody(inputs, res.APIPropertyNames)
	path := substituteAllParams(res.CreatePath, "", res.IDPathParam, body)
	respBody, err := c.request(ctx, res.CreateMethod, path, body)
	if err != nil {
		return "", property.Map{}, fmt.Errorf("create %s: %w", res.Name, err)
	}

	id := extractID(respBody, res.IDField, res.IDPathParam)
	if id == "" {
		return "", property.Map{}, fmt.Errorf("create %s: could not extract ID from response (looked for field %q)", res.Name, res.IDField)
	}

	if c.pollingEnabled {
		// Seed polling state with inputs so context path params (e.g. {organisationId})
		// are available for URL substitution even when the create response omits them.
		// Response fields overwrite inputs so no response data is lost.
		pollingState := propertyMapToAPIBody(inputs, res.APIPropertyNames)
		maps.Copy(pollingState, propertyMapToGoMap(apiBodyToPropertyMap(respBody, res.APIPropertyNames)))
		if err := c.waitUntilExists(ctx, res, id, pollingState); err != nil {
			return "", property.Map{}, fmt.Errorf("create %s: %w", res.Name, err)
		}
		// Re-read to get fully-populated state after the resource is confirmed to exist.
		outputs, err := c.read(ctx, res, id, pollingState)
		if err != nil {
			return "", property.Map{}, fmt.Errorf("create %s: post-create read: %w", res.Name, err)
		}
		return id, outputs, nil
	}

	outputs := apiBodyToPropertyMap(respBody, res.APIPropertyNames)
	return id, outputs, nil
}

// read calls GET on the read endpoint and returns the current state.
// Returns empty state if the resource no longer exists (404).
func (c *crudClient) read(ctx context.Context, res spec.ResourceDef, id string, state map[string]any) (property.Map, error) {
	path := substituteAllParams(res.ReadPath, id, res.IDPathParam, state)
	respBody, err := c.requestNoBody(ctx, "GET", path)
	if err != nil {
		if isNotFound(err) {
			return property.Map{}, nil
		}
		return property.Map{}, fmt.Errorf("read %s: %w", res.Name, err)
	}
	return apiBodyToPropertyMap(respBody, res.APIPropertyNames), nil
}

// update calls PUT or PATCH on the update endpoint.
func (c *crudClient) update(ctx context.Context, res spec.ResourceDef, id string, inputs property.Map) (property.Map, error) {
	if res.UpdatePath == "" {
		return inputs, nil
	}
	body := propertyMapToAPIBody(inputs, res.APIPropertyNames)
	path := substituteAllParams(res.UpdatePath, id, res.IDPathParam, body)
	respBody, err := c.request(ctx, res.UpdateMethod, path, body)
	if err != nil {
		return property.Map{}, fmt.Errorf("update %s: %w", res.Name, err)
	}
	return apiBodyToPropertyMap(respBody, res.APIPropertyNames), nil
}

// del calls DELETE on the delete endpoint.
func (c *crudClient) del(ctx context.Context, res spec.ResourceDef, id string, state map[string]any) error {
	path := substituteAllParams(res.DeletePath, id, res.IDPathParam, state)
	_, err := c.requestNoBody(ctx, "DELETE", path)
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete %s: %w", res.Name, err)
	}

	if c.pollingEnabled {
		if err := c.waitUntilGone(ctx, res, id, state); err != nil {
			return fmt.Errorf("delete %s: %w", res.Name, err)
		}
	}
	return nil
}

// waitUntilExists polls the read endpoint until the resource returns a non-empty response
// or the polling timeout is reached.
func (c *crudClient) waitUntilExists(ctx context.Context, res spec.ResourceDef, id string, state map[string]any) error {
	return c.pollUntil(ctx, func() (bool, error) {
		outputs, err := c.read(ctx, res, id, state)
		if err != nil {
			return false, err
		}
		return outputs.Len() > 0, nil
	}, fmt.Sprintf("timed out waiting for %s %q to exist", res.Name, id))
}

// waitUntilGone polls the read endpoint until the resource returns 404 (empty outputs)
// or the polling timeout is reached.
func (c *crudClient) waitUntilGone(ctx context.Context, res spec.ResourceDef, id string, state map[string]any) error {
	return c.pollUntil(ctx, func() (bool, error) {
		outputs, err := c.read(ctx, res, id, state)
		if err != nil {
			return false, err
		}
		return outputs.Len() == 0, nil
	}, fmt.Sprintf("timed out waiting for %s %q to be deleted", res.Name, id))
}

// pollUntil runs condition in a backoff loop until it returns true, the timeout expires,
// or ctx is cancelled.
func (c *crudClient) pollUntil(ctx context.Context, condition func() (bool, error), timeoutMsg string) error {
	deadline := time.Now().Add(c.polling.Timeout)
	interval := c.polling.InitialInterval
	for {
		done, err := condition()
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("%s", timeoutMsg)
		}
		sleep := min(interval, remaining)
		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		interval = min(time.Duration(float64(interval)*c.polling.Multiplier), c.polling.MaxInterval)
	}
}

// request performs an HTTP call with a JSON body and returns the decoded response.
func (c *crudClient) request(ctx context.Context, method, path string, body map[string]any) (map[string]any, error) {
	if c.cfg.GetBaseURL() == "" {
		return nil, fmt.Errorf("baseUrl is not set: provide it via provider config or ensure the spec declares a server URL")
	}

	var buf *bytes.Buffer
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request body: %w", err)
		}
		buf = bytes.NewBuffer(data)
	} else {
		buf = bytes.NewBuffer(nil)
	}

	url := c.cfg.GetBaseURL() + path
	req, err := http.NewRequestWithContext(ctx, method, url, buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	for k, v := range c.cfg.AuthHeaders() {
		req.Header.Set(k, v)
	}

	return c.do(req)
}

// requestNoBody performs an HTTP call without a request body.
func (c *crudClient) requestNoBody(ctx context.Context, method, path string) (map[string]any, error) {
	if c.cfg.GetBaseURL() == "" {
		return nil, fmt.Errorf("baseUrl is not set: provide it via provider config or ensure the spec declares a server URL")
	}

	url := c.cfg.GetBaseURL() + path
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range c.cfg.AuthHeaders() {
		req.Header.Set(k, v)
	}
	return c.do(req)
}

type notFoundError struct{ status int }

func (e *notFoundError) Error() string { return fmt.Sprintf("HTTP %d", e.status) }

func isNotFound(err error) bool {
	if nfe, ok := err.(*notFoundError); ok { //nolint:errorlint
		return nfe.status == 404
	}
	return false
}

func (c *crudClient) do(req *http.Request) (map[string]any, error) {
	resp, err := c.cfg.Client().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == 404 {
		return nil, &notFoundError{status: 404}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if len(data) == 0 {
		return map[string]any{}, nil
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return result, nil
}

// substituteAllParams replaces all {param} placeholders in path.
// The resource ID is substituted first (using id + idParam), then any remaining
// placeholders are filled from vals (the resource inputs or state map).
func substituteAllParams(path, id, idParam string, vals map[string]any) string {
	result := path
	if idParam != "" && id != "" {
		result = strings.ReplaceAll(result, "{"+idParam+"}", id)
	}
	// Replace remaining placeholders from the values map.
	for {
		start := strings.Index(result, "{")
		end := strings.Index(result, "}")
		if start == -1 || end <= start {
			break
		}
		name := result[start+1 : end]
		val := ""
		if v, ok := vals[name]; ok {
			val = fmt.Sprintf("%v", v)
		}
		result = result[:start] + val + result[end+1:]
	}
	return result
}

// extractID pulls the resource ID from a JSON response map.
// It tries idField first, then idPathParam, then "id".
// Keys may use dot notation to traverse nested objects (e.g. "metadata.name").
func extractID(body map[string]any, idField, idPathParam string) string {
	candidates := []string{idField, idPathParam, "id"}
	for _, key := range candidates {
		if key == "" {
			continue
		}
		if v := nestedGet(body, key); v != "" {
			return v
		}
	}
	return ""
}

// nestedGet resolves a dot-separated key path into a map, e.g. "metadata.name".
func nestedGet(m map[string]any, path string) string {
	parts := strings.SplitN(path, ".", 2)
	v, ok := m[parts[0]]
	if !ok {
		return ""
	}
	if len(parts) == 1 {
		return fmt.Sprintf("%v", v)
	}
	nested, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	return nestedGet(nested, parts[1])
}

// propertyMapToAPIBody converts a property.Map to a plain Go map, translating camelCase
// Pulumi property keys back to their original API names using the aliases map.
func propertyMapToAPIBody(m property.Map, aliases map[string]string) map[string]any {
	result := map[string]any{}
	m.All(func(key string, val property.Value) bool {
		apiKey := key
		if aliases != nil {
			if orig, ok := aliases[key]; ok {
				apiKey = orig
			}
		}
		result[apiKey] = propertyValueToGo(val)
		return true
	})
	return result
}

// apiBodyToPropertyMap converts an API response map to a property.Map, translating API
// property names to camelCase Pulumi names using the reverse of the aliases map.
// Nested object keys are converted generically with toCamelCase.
func apiBodyToPropertyMap(m map[string]any, aliases map[string]string) property.Map {
	reverse := make(map[string]string, len(aliases))
	for camel, api := range aliases {
		reverse[api] = camel
	}
	vals := map[string]property.Value{}
	for k, v := range m {
		pulumiKey := k
		if camel, ok := reverse[k]; ok {
			pulumiKey = camel
		}
		vals[pulumiKey] = goValueToProperty(v)
	}
	return property.NewMap(vals)
}

// propertyMapToGoMap converts a property.Map to a plain Go map for JSON marshaling.
func propertyMapToGoMap(m property.Map) map[string]any {
	result := map[string]any{}
	m.All(func(key string, val property.Value) bool {
		result[key] = propertyValueToGo(val)
		return true
	})
	return result
}

func propertyValueToGo(v property.Value) any {
	switch {
	case v.IsNull():
		return nil
	case v.IsBool():
		return v.AsBool()
	case v.IsNumber():
		return v.AsNumber()
	case v.IsString():
		return v.AsString()
	case v.IsArray():
		arr := v.AsArray()
		result := make([]any, arr.Len())
		for i, item := range arr.AsSlice() {
			result[i] = propertyValueToGo(item)
		}
		return result
	case v.IsMap():
		return propertyMapToGoMap(v.AsMap())
	default:
		return nil
	}
}

// goMapToPropertyMap converts a plain Go map (from JSON) to a property.Map.
func goMapToPropertyMap(m map[string]any) property.Map {
	vals := map[string]property.Value{}
	for k, v := range m {
		vals[k] = goValueToProperty(v)
	}
	return property.NewMap(vals)
}

func goValueToProperty(v any) property.Value {
	if v == nil {
		return property.New(property.Null)
	}
	switch tv := v.(type) {
	case bool:
		return property.New(tv)
	case float64:
		return property.New(tv)
	case string:
		return property.New(tv)
	case []any:
		items := make([]property.Value, len(tv))
		for i, item := range tv {
			items[i] = goValueToProperty(item)
		}
		return property.New(property.NewArray(items))
	case map[string]any:
		return property.New(goMapToPropertyMap(tv))
	default:
		return property.New(fmt.Sprintf("%v", v))
	}
}

// mergeInputsIntoOutputs adds any input values not already in outputs to the output map.
func mergeInputsIntoOutputs(inputs, outputs property.Map) property.Map {
	result := map[string]property.Value{}
	outputs.All(func(k string, v property.Value) bool {
		result[k] = v
		return true
	})
	inputs.All(func(k string, v property.Value) bool {
		if _, exists := result[k]; !exists {
			result[k] = v
		}
		return true
	})
	return property.NewMap(result)
}

// setID adds the resource ID to a property map under its field name.
func setID(outputs property.Map, idField, id string) property.Map {
	if idField == "" {
		return outputs
	}
	return outputs.Set(idField, property.New(id))
}

// handleCreate performs a create operation using the CRUD client.
func handleCreate(ctx context.Context, res spec.ResourceDef, req p.CreateRequest, client *crudClient) (p.CreateResponse, error) {
	if req.DryRun {
		return p.CreateResponse{ID: "", Properties: req.Properties}, nil
	}
	id, outputs, err := client.create(ctx, res, req.Properties)
	if err != nil {
		return p.CreateResponse{}, err
	}
	outputs = mergeInputsIntoOutputs(req.Properties, outputs)
	outputs = setID(outputs, res.IDField, id)
	return p.CreateResponse{ID: id, Properties: outputs}, nil
}

// handleRead performs a read operation using the CRUD client.
func handleRead(ctx context.Context, res spec.ResourceDef, req p.ReadRequest, cfg *config.ProviderConfig) (p.ReadResponse, error) {
	client := &crudClient{cfg: cfg}
	state := propertyMapToGoMap(req.Properties)
	outputs, err := client.read(ctx, res, req.ID, state)
	if err != nil {
		return p.ReadResponse{}, err
	}
	return p.ReadResponse{ID: req.ID, Properties: outputs}, nil
}

// handleUpdate performs an update operation using the CRUD client.
func handleUpdate(ctx context.Context, res spec.ResourceDef, req p.UpdateRequest, cfg *config.ProviderConfig) (p.UpdateResponse, error) {
	client := &crudClient{cfg: cfg}
	outputs, err := client.update(ctx, res, req.ID, req.Inputs)
	if err != nil {
		return p.UpdateResponse{}, err
	}
	outputs = mergeInputsIntoOutputs(req.Inputs, outputs)
	return p.UpdateResponse{Properties: outputs}, nil
}

// handleDelete performs a delete operation using the CRUD client.
func handleDelete(ctx context.Context, res spec.ResourceDef, req p.DeleteRequest, client *crudClient) error {
	state := propertyMapToGoMap(req.Properties)
	return client.del(ctx, res, req.ID, state)
}
