package spec

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pb33f/libopenapi"
	highbase "github.com/pb33f/libopenapi/datamodel/high/base"
	v2high "github.com/pb33f/libopenapi/datamodel/high/v2"
	v3high "github.com/pb33f/libopenapi/datamodel/high/v3"
	pschema "github.com/pulumi/pulumi/pkg/v3/codegen/schema"
)

// AuthScheme describes a single security scheme discovered from an OpenAPI spec.
type AuthScheme struct {
	Kind        string // "apiKey", "bearer", "basic"
	ConfigVar   string // Pulumi config var name (empty for basic — uses "username"/"password")
	HeaderName  string // HTTP header to set (apiKey in header, bearer); empty for query/basic
	QueryParam  string // query parameter name (apiKey in query)
	Description string
	Secret      bool
}

// ResourceDef describes a Pulumi resource discovered from an OpenAPI spec.
type ResourceDef struct {
	Name         string // e.g. "Pet", "StoreOrder"
	Token        string // e.g. "mypkg:index:Pet"
	CreatePath   string // e.g. "/pet"
	CreateMethod string // e.g. "POST"
	ReadPath     string // e.g. "/pet/{petId}"
	UpdatePath   string // e.g. "/pet/{petId}" — empty if not available
	UpdateMethod string // e.g. "PUT" or "PATCH"
	DeletePath   string // e.g. "/pet/{petId}"
	IDPathParam  string // path param name used as resource ID, e.g. "petId"
	IDField      string // JSON field name holding the server-assigned ID, e.g. "id"

	InputSchema      map[string]pschema.PropertySpec
	OutputSchema     map[string]pschema.PropertySpec
	RequiredInputs   []string
	APIPropertyNames map[string]string // camelCase Pulumi name → original API name, e.g. {"syncBehavior": "sync_behavior"}
}

// DiscoveryResult contains all resources and shared types discovered from the spec.
type DiscoveryResult struct {
	Resources      []ResourceDef
	Types          map[string]pschema.ComplexTypeSpec
	DefaultBaseURL string
	AuthSchemes    []AuthScheme
}

// Discover identifies Pulumi resources from an OpenAPI document using path conventions.
// Supports both Swagger 2.0 and OpenAPI 3.x specs.
func Discover(doc libopenapi.Document, pkgName string, overrides map[string]ResourceOverride) (DiscoveryResult, error) {
	info := doc.GetSpecInfo()
	if info.SpecFormat == "oas2" {
		return discoverV2(doc, pkgName, overrides)
	}
	return discoverV3(doc, pkgName, overrides)
}

// ResourceOverride is re-declared here to avoid circular imports; callers pass the openapi.ResourceOverride.
// This matches the field layout of openapi.ResourceOverride exactly.
type ResourceOverride struct {
	Skip         bool
	Token        string
	CreatePath   string
	CreateMethod string
	ReadPath     string
	UpdatePath   string
	UpdateMethod string
	DeletePath   string
	IDPathParam  string
	IDField      string
}

type pathGroup struct {
	name           string // resource name, e.g. "Pet"
	collectionPath string // e.g. "/pet" or "/api/orgs/{orgName}/tokens"
	itemPath       string // e.g. "/pet/{petId}" or "/api/orgs/{orgName}/tokens/{tokenId}"
	idPathParam    string // the trailing param name, e.g. "petId" or "tokenId"
}

func discoverV2(doc libopenapi.Document, pkgName string, overrides map[string]ResourceOverride) (DiscoveryResult, error) {
	model, err := doc.BuildV2Model()
	if err != nil && model == nil {
		return DiscoveryResult{}, fmt.Errorf("building v2 model: %w", err)
	}
	swagger := &model.Model

	baseURL := extractBaseURLV2(swagger)
	rootTags := rootTagSet(swagger.Tags)
	typeCollector := &typeCollector{
		pkgName: pkgName,
		defs:    swagger.Definitions,
		types:   map[string]pschema.ComplexTypeSpec{},
	}

	groups := groupPaths(swagger)
	var resources []ResourceDef

	for _, g := range groups {
		name := g.name
		or, hasOverride := overrides[name]
		if hasOverride && or.Skip {
			continue
		}

		res, ok := buildResourceV2(g, swagger, pkgName, rootTags, typeCollector)
		if !ok {
			continue
		}

		// Apply overrides
		if hasOverride {
			applyOverride(&res, or)
		}

		resources = append(resources, res)
	}

	return DiscoveryResult{
		Resources:      resources,
		Types:          typeCollector.types,
		DefaultBaseURL: baseURL,
		AuthSchemes:    extractAuthSchemesV2(swagger),
	}, nil
}

func extractAuthSchemesV2(swagger *v2high.Swagger) []AuthScheme {
	if swagger.SecurityDefinitions == nil || swagger.SecurityDefinitions.Definitions == nil {
		return nil
	}
	var schemes []AuthScheme
	for pair := swagger.SecurityDefinitions.Definitions.Oldest(); pair != nil; pair = pair.Next() {
		key := pair.Key
		s := pair.Value
		switch s.Type {
		case "apiKey":
			scheme := AuthScheme{
				Kind:        "apiKey",
				ConfigVar:   lowercaseFirst(key),
				Description: s.Description,
				Secret:      true,
			}
			if s.In == "header" {
				scheme.HeaderName = s.Name
			} else if s.In == "query" {
				scheme.QueryParam = s.Name
			}
			schemes = append(schemes, scheme)
		case "basic":
			schemes = append(schemes, AuthScheme{
				Kind:        "basic",
				Description: s.Description,
			})
		case "oauth2":
			schemes = append(schemes, AuthScheme{
				Kind:        "bearer",
				ConfigVar:   "bearerToken",
				HeaderName:  "Authorization",
				Description: s.Description,
				Secret:      true,
			})
		}
	}
	return schemes
}

func extractBaseURLV2(swagger *v2high.Swagger) string {
	scheme := "https"
	if len(swagger.Schemes) > 0 {
		for _, s := range swagger.Schemes {
			if s == "https" {
				scheme = "https"
				break
			}
			scheme = s
		}
	}
	host := swagger.Host
	if host == "" {
		host = "localhost"
	}
	base := swagger.BasePath
	if base == "/" {
		base = ""
	}
	return scheme + "://" + host + base
}

// groupPaths groups the spec paths by resource using the static prefix convention.
func groupPaths(swagger *v2high.Swagger) []pathGroup {
	if swagger.Paths == nil || swagger.Paths.PathItems == nil {
		return nil
	}
	var paths []string
	for pair := swagger.Paths.PathItems.Oldest(); pair != nil; pair = pair.Next() {
		paths = append(paths, pair.Key)
	}
	return groupPathStrings(paths)
}

// groupPathStrings groups API paths into (collection, item) pairs for resource discovery.
//
// Strategy: for every path ending in {param}, treat it as an item path whose collection
// is the parent (all segments except the last). This handles both simple paths like
// /pet/{petId} and scoped paths like /api/orgs/{orgName}/tokens/{tokenId}.
// Deeper paths are processed first; once a path is used as a collection it cannot
// also be treated as an item to avoid duplicate shallow groups.
func groupPathStrings(paths []string) []pathGroup {
	// Sort deepest first so a path used as a collection for a deeper item
	// gets claimed before it can be independently treated as an item.
	sorted := make([]string, len(paths))
	copy(sorted, paths)
	sort.Slice(sorted, func(i, j int) bool {
		return strings.Count(sorted[i], "/") > strings.Count(sorted[j], "/")
	})

	// claimedCollection tracks paths already in use as a collection path so that
	// the same path is not also emitted as an item for a shallower group.
	claimedCollection := map[string]bool{}

	seen := map[string]bool{} // deduplicate by item path
	var groups []pathGroup

	for _, rawPath := range sorted {
		segs := splitSegs(rawPath)
		if len(segs) == 0 {
			continue
		}
		last := segs[len(segs)-1]
		if !strings.HasPrefix(last, "{") || !strings.HasSuffix(last, "}") {
			continue // not an item path
		}
		if claimedCollection[rawPath] {
			continue // already used as a collection for a deeper group
		}
		if seen[rawPath] {
			continue
		}
		seen[rawPath] = true

		idParam := last[1 : len(last)-1]
		parentSegs := segs[:len(segs)-1]
		collectionPath := "/" + strings.Join(parentSegs, "/")

		name := buildNameFromSegs(parentSegs)
		if name == "" {
			continue
		}

		groups = append(groups, pathGroup{
			name:           name,
			collectionPath: collectionPath,
			itemPath:       rawPath,
			idPathParam:    idParam,
		})
		claimedCollection[collectionPath] = true
	}
	return groups
}

// splitSegs splits a URL path into non-empty segments.
func splitSegs(path string) []string {
	var segs []string
	for _, s := range strings.Split(strings.Trim(path, "/"), "/") {
		if s != "" {
			segs = append(segs, s)
		}
	}
	return segs
}

// buildNameFromSegs returns a PascalCase name from path segments, skipping {param} segments.
// Kebab-case and snake_case segments are split on "-" and "_" so each word is capitalised.
func buildNameFromSegs(segs []string) string {
	var parts []string
	for _, seg := range segs {
		if seg == "" || strings.HasPrefix(seg, "{") {
			continue
		}
		for _, word := range splitWords(seg) {
			if word != "" {
				parts = append(parts, capitalize(word))
			}
		}
	}
	return strings.Join(parts, "")
}

// splitWords splits a path segment on hyphens and underscores.
func splitWords(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' })
}

// toPascalCase converts a snake_case or kebab-case string to PascalCase.
func toPascalCase(s string) string {
	var parts []string
	for _, w := range splitWords(s) {
		if w != "" {
			parts = append(parts, capitalize(w))
		}
	}
	if len(parts) == 0 {
		return s
	}
	return strings.Join(parts, "")
}

// toCamelCase converts a snake_case or kebab-case string to camelCase.
// If the string contains no separators it is assumed to already be camelCase or
// PascalCase and only the first character is lowercased.
func toCamelCase(s string) string {
	if !strings.ContainsAny(s, "-_") {
		return lowercaseFirst(s)
	}
	words := splitWords(s)
	if len(words) == 0 {
		return s
	}
	var b strings.Builder
	b.WriteString(strings.ToLower(words[0]))
	for _, w := range words[1:] {
		b.WriteString(capitalize(w))
	}
	return b.String()
}

// contextPathParams returns path parameter names found in path that are not the resource ID.
// These are "parent scope" params like {orgName} that the user must supply as inputs.
func contextPathParams(path, idPathParam string) []string {
	var params []string
	seen := map[string]bool{}
	for _, seg := range splitSegs(path) {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			name := seg[1 : len(seg)-1]
			if name != idPathParam && !seen[name] {
				seen[name] = true
				params = append(params, name)
			}
		}
	}
	return params
}

func capitalize(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func lowercaseFirst(s string) string {
	if s == "" {
		return ""
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// buildResourceV2 constructs a ResourceDef from a path group and Swagger spec.
func buildResourceV2(g pathGroup, swagger *v2high.Swagger, pkgName string, rootTags map[string]bool, tc *typeCollector) (ResourceDef, bool) {
	if swagger.Paths == nil || swagger.Paths.PathItems == nil {
		return ResourceDef{}, false
	}

	var collectionItem *v2high.PathItem
	var itemItem *v2high.PathItem

	if g.collectionPath != "" {
		if pi, ok := swagger.Paths.PathItems.Get(g.collectionPath); ok {
			collectionItem = pi
		}
	}
	if g.itemPath != "" {
		if pi, ok := swagger.Paths.PathItems.Get(g.itemPath); ok {
			itemItem = pi
		}
	}

	// Detect CRUD operations
	var createOp *v2high.Operation
	var readOp *v2high.Operation
	var updateOp *v2high.Operation
	var updateMethod string
	var deleteOp *v2high.Operation

	if collectionItem != nil && collectionItem.Post != nil {
		createOp = collectionItem.Post
	}
	if itemItem != nil {
		if itemItem.Get != nil {
			readOp = itemItem.Get
		}
		if itemItem.Put != nil {
			updateOp = itemItem.Put
			updateMethod = "PUT"
		} else if itemItem.Patch != nil {
			updateOp = itemItem.Patch
			updateMethod = "PATCH"
		}
		if itemItem.Delete != nil {
			deleteOp = itemItem.Delete
		}
	}

	// Skip resources that can't be managed (need at least Create and one of Read/Delete)
	if createOp == nil || (readOp == nil && deleteOp == nil) {
		return ResourceDef{}, false
	}

	module := moduleFromOps(rootTags, v2OpTags(createOp), v2OpTags(readOp), v2OpTags(updateOp), v2OpTags(deleteOp))
	token := fmt.Sprintf("%s:%s:%s", pkgName, module, g.name)
	idPathParam := g.idPathParam

	// Extract input schema from the create operation's body parameter
	var createSchema *highbase.Schema
	if createOp != nil {
		for _, param := range createOp.Parameters {
			if param.In == "body" && param.Schema != nil {
				createSchema = resolveSchema(param.Schema, swagger.Definitions)
				break
			}
		}
	}

	// Extract output schema from the read operation's 200 response
	var readSchema *highbase.Schema
	if readOp != nil {
		readSchema = extractResponseSchema(readOp, swagger.Definitions)
	} else if createOp != nil {
		readSchema = extractResponseSchema(createOp, swagger.Definitions)
	}

	// Build input and output properties
	inputs := map[string]pschema.PropertySpec{}
	outputs := map[string]pschema.PropertySpec{}
	apiPropertyNames := map[string]string{}
	var requiredInputs []string

	if createSchema != nil {
		for pair := createSchema.Properties.Oldest(); pair != nil; pair = pair.Next() {
			apiName := pair.Key
			camelName := toCamelCase(apiName)
			spec := tc.convertProperty(pair.Value, swagger.Definitions)
			inputs[camelName] = spec
			outputs[camelName] = spec
			apiPropertyNames[camelName] = apiName
		}
		for _, r := range filterRequired(createSchema.Required, idPathParam) {
			requiredInputs = append(requiredInputs, toCamelCase(r))
		}
	}

	// Merge in output-only properties from the read schema
	if readSchema != nil {
		for pair := readSchema.Properties.Oldest(); pair != nil; pair = pair.Next() {
			apiName := pair.Key
			camelName := toCamelCase(apiName)
			if _, alreadyInput := inputs[camelName]; !alreadyInput {
				spec := tc.convertProperty(pair.Value, swagger.Definitions)
				outputs[camelName] = spec
				apiPropertyNames[camelName] = apiName
			}
		}
	}

	// ID field: use the path param name by default, or find a matching field in read schema
	idField := idPathParam
	if idField != "" {
		// Look for a field in the read schema that matches the path param (strip trailing "Id" etc.)
		if readSchema != nil {
			if _, ok := readSchema.Properties.Get("id"); ok {
				idField = "id"
			}
		}
		// Remove the ID field from both inputs and outputs: it is server-assigned and
		// "id" is a reserved property name in Pulumi (available automatically on all resources).
		delete(inputs, idField)
		delete(outputs, idField)
		delete(inputs, toCamelCase(idField))
		delete(outputs, toCamelCase(idField))
	}
	// Always remove "id" from outputs — Pulumi reserves it as a built-in resource property.
	delete(outputs, "id")

	// Remove ID from required inputs (it is server-assigned)
	if idField != "" {
		requiredInputs = removeFromSlice(requiredInputs, idField)
		requiredInputs = removeFromSlice(requiredInputs, toCamelCase(idField))
		requiredInputs = removeFromSlice(requiredInputs, idPathParam)
		requiredInputs = removeFromSlice(requiredInputs, toCamelCase(idPathParam))
	}

	// Add context path params (e.g. {orgName}) that are not in the body schema.
	// Users must supply these to construct the API URL.
	for _, ctxParam := range contextPathParams(g.itemPath, idPathParam) {
		if _, exists := inputs[ctxParam]; !exists {
			inputs[ctxParam] = pschema.PropertySpec{TypeSpec: pschema.TypeSpec{Type: "string"}}
			outputs[ctxParam] = pschema.PropertySpec{TypeSpec: pschema.TypeSpec{Type: "string"}}
			requiredInputs = append(requiredInputs, ctxParam)
		}
	}

	updatePath := ""
	if updateOp != nil {
		updatePath = g.itemPath
	}

	return ResourceDef{
		Name:             g.name,
		Token:            token,
		CreatePath:       g.collectionPath,
		CreateMethod:     "POST",
		ReadPath:         g.itemPath,
		UpdatePath:       updatePath,
		UpdateMethod:     updateMethod,
		DeletePath:       g.itemPath,
		IDPathParam:      idPathParam,
		IDField:          idField,
		InputSchema:      inputs,
		OutputSchema:     outputs,
		RequiredInputs:   requiredInputs,
		APIPropertyNames: apiPropertyNames,
	}, true
}

// extractResponseSchema returns the schema from the first 2xx response of an operation.
func extractResponseSchema(op *v2high.Operation, defs *v2high.Definitions) *highbase.Schema {
	if op == nil || op.Responses == nil {
		return nil
	}
	for _, code := range []string{"200", "201", "202"} {
		if resp, ok := op.Responses.Codes.Get(code); ok && resp.Schema != nil {
			return resolveSchema(resp.Schema, defs)
		}
	}
	return nil
}

// resolveSchema dereferences a SchemaProxy, resolving $ref if present.
func resolveSchema(proxy *highbase.SchemaProxy, defs *v2high.Definitions) *highbase.Schema {
	if proxy == nil {
		return nil
	}
	ref := proxy.GetReference()
	if ref != "" && defs != nil {
		// $ref is like "#/definitions/Pet"
		defName := extractDefName(ref)
		if defName != "" {
			if defProxy, ok := defs.Definitions.Get(defName); ok {
				return defProxy.Schema()
			}
		}
	}
	return proxy.Schema()
}

func extractDefName(ref string) string {
	const prefix = "#/definitions/"
	if strings.HasPrefix(ref, prefix) {
		return ref[len(prefix):]
	}
	return ""
}

func filterRequired(required []string, exclude string) []string {
	var out []string
	for _, r := range required {
		if r != exclude {
			out = append(out, r)
		}
	}
	return out
}

func removeFromSlice(s []string, val string) []string {
	var out []string
	for _, v := range s {
		if v != val {
			out = append(out, v)
		}
	}
	return out
}

// rootTagSet returns the set of tag names defined at the root of the spec.
func rootTagSet(tags []*highbase.Tag) map[string]bool {
	set := map[string]bool{}
	for _, t := range tags {
		if t != nil && t.Name != "" {
			set[t.Name] = true
		}
	}
	return set
}

// moduleFromOps returns the first operation tag that exists in rootTags, or "index".
// The tag is sanitised to a lowercase alphanumeric string suitable for a Pulumi module name.
func moduleFromOps(rootTags map[string]bool, tagLists ...[]string) string {
	for _, tags := range tagLists {
		for _, tag := range tags {
			if rootTags[tag] {
				return sanitiseModule(tag)
			}
		}
	}
	return "index"
}

// sanitiseModule converts a tag name to a lowercase alphanumeric Pulumi module name.
// Spaces, hyphens, underscores and other non-alphanumeric characters are dropped.
// e.g. "AI Content" → "aicontent", "Away Status Reasons" → "awaystatusreasons"
func sanitiseModule(tag string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(tag) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func v2OpTags(op *v2high.Operation) []string {
	if op == nil {
		return nil
	}
	return op.Tags
}

func v3OpTags(op *v3high.Operation) []string {
	if op == nil {
		return nil
	}
	return op.Tags
}

func applyOverride(res *ResourceDef, or ResourceOverride) {
	if or.Token != "" {
		res.Token = or.Token
	}
	if or.CreatePath != "" {
		res.CreatePath = or.CreatePath
	}
	if or.CreateMethod != "" {
		res.CreateMethod = or.CreateMethod
	}
	if or.ReadPath != "" {
		res.ReadPath = or.ReadPath
	}
	if or.UpdatePath != "" {
		res.UpdatePath = or.UpdatePath
	}
	if or.UpdateMethod != "" {
		res.UpdateMethod = or.UpdateMethod
	}
	if or.DeletePath != "" {
		res.DeletePath = or.DeletePath
	}
	if or.IDPathParam != "" {
		res.IDPathParam = or.IDPathParam
	}
	if or.IDField != "" {
		res.IDField = or.IDField
	}
}

// typeCollector resolves and accumulates shared object types encountered during discovery.
type typeCollector struct {
	pkgName string
	defs    *v2high.Definitions
	types   map[string]pschema.ComplexTypeSpec
}

// convertProperty converts a SchemaProxy to a Pulumi PropertySpec.
func (tc *typeCollector) convertProperty(proxy *highbase.SchemaProxy, defs *v2high.Definitions) pschema.PropertySpec {
	if proxy == nil {
		return pschema.PropertySpec{TypeSpec: pschema.TypeSpec{Type: "string"}}
	}
	ref := proxy.GetReference()
	if ref != "" {
		defName := extractDefName(ref)
		if defName != "" {
			tc.ensureType(defName, defs)
			return pschema.PropertySpec{
				TypeSpec: pschema.TypeSpec{
					Ref: fmt.Sprintf("#/types/%s:index:%s", tc.pkgName, toPascalCase(defName)),
				},
			}
		}
	}
	schema := proxy.Schema()
	return tc.convertSchema(schema, defs)
}

func (tc *typeCollector) convertSchema(schema *highbase.Schema, defs *v2high.Definitions) pschema.PropertySpec {
	if schema == nil {
		return pschema.PropertySpec{TypeSpec: pschema.TypeSpec{Type: "string"}}
	}

	t := ""
	if len(schema.Type) > 0 {
		t = schema.Type[0]
	}

	switch t {
	case "integer":
		return pschema.PropertySpec{
			TypeSpec:    pschema.TypeSpec{Type: "integer"},
			Description: schema.Description,
		}
	case "number":
		return pschema.PropertySpec{
			TypeSpec:    pschema.TypeSpec{Type: "number"},
			Description: schema.Description,
		}
	case "boolean":
		return pschema.PropertySpec{
			TypeSpec:    pschema.TypeSpec{Type: "boolean"},
			Description: schema.Description,
		}
	case "array":
		itemSpec := tc.arrayItemSpec(schema, defs)
		return pschema.PropertySpec{
			TypeSpec: pschema.TypeSpec{
				Type:  "array",
				Items: &itemSpec,
			},
			Description: schema.Description,
		}
	case "object":
		return pschema.PropertySpec{
			TypeSpec:    pschema.TypeSpec{Type: "object"},
			Description: schema.Description,
		}
	default:
		// default to string
		return pschema.PropertySpec{
			TypeSpec:    pschema.TypeSpec{Type: "string"},
			Description: schema.Description,
		}
	}
}

func (tc *typeCollector) arrayItemSpec(schema *highbase.Schema, defs *v2high.Definitions) pschema.TypeSpec {
	if schema.Items == nil {
		return pschema.TypeSpec{Type: "string"}
	}
	itemProxy := schema.Items.A
	if itemProxy == nil {
		return pschema.TypeSpec{Type: "string"}
	}
	ref := itemProxy.GetReference()
	if ref != "" {
		defName := extractDefName(ref)
		if defName != "" {
			tc.ensureType(defName, defs)
			return pschema.TypeSpec{
				Ref: fmt.Sprintf("#/types/%s:index:%s", tc.pkgName, toPascalCase(defName)),
			}
		}
	}
	itemSchema := itemProxy.Schema()
	if itemSchema == nil {
		return pschema.TypeSpec{Type: "string"}
	}
	if len(itemSchema.Type) == 0 {
		return pschema.TypeSpec{Type: "string"}
	}
	return pschema.TypeSpec{Type: itemSchema.Type[0]}
}

// ensureType adds the named definition to the types map if not already present.
func (tc *typeCollector) ensureType(defName string, defs *v2high.Definitions) {
	if defs == nil {
		return
	}
	token := fmt.Sprintf("%s:index:%s", tc.pkgName, toPascalCase(defName))
	if _, exists := tc.types[token]; exists {
		return
	}
	defProxy, ok := defs.Definitions.Get(defName)
	if !ok {
		return
	}
	defSchema := defProxy.Schema()
	if defSchema == nil {
		return
	}

	props := map[string]pschema.PropertySpec{}
	if defSchema.Properties != nil {
		for pair := defSchema.Properties.Oldest(); pair != nil; pair = pair.Next() {
			props[toCamelCase(pair.Key)] = tc.convertProperty(pair.Value, defs)
		}
	}

	// Register with a placeholder first to handle recursive types
	tc.types[token] = pschema.ComplexTypeSpec{
		ObjectTypeSpec: pschema.ObjectTypeSpec{
			Type:        "object",
			Description: defSchema.Description,
			Properties:  props,
			Required:    defSchema.Required,
		},
	}
}

// ---------------------------------------------------------------------------
// OpenAPI 3.x support
// ---------------------------------------------------------------------------

func discoverV3(doc libopenapi.Document, pkgName string, overrides map[string]ResourceOverride) (DiscoveryResult, error) {
	model, err := doc.BuildV3Model()
	if err != nil && model == nil {
		return DiscoveryResult{}, fmt.Errorf("building v3 model: %w", err)
	}
	d := &model.Model

	baseURL := extractBaseURLV3(d)
	rootTags := rootTagSet(d.Tags)
	tc := &typeCollectorV3{
		pkgName:    pkgName,
		components: d.Components,
		types:      map[string]pschema.ComplexTypeSpec{},
	}

	var pathKeys []string
	if d.Paths != nil && d.Paths.PathItems != nil {
		for k, _ := range d.Paths.PathItems.FromOldest() {
			pathKeys = append(pathKeys, k)
		}
	}
	groups := groupPathStrings(pathKeys)

	var resources []ResourceDef
	for _, g := range groups {
		or, hasOverride := overrides[g.name]
		if hasOverride && or.Skip {
			continue
		}
		res, ok := buildResourceV3(g, d, pkgName, rootTags, tc)
		if !ok {
			continue
		}
		if hasOverride {
			applyOverride(&res, or)
		}
		resources = append(resources, res)
	}

	return DiscoveryResult{
		Resources:      resources,
		Types:          tc.types,
		DefaultBaseURL: baseURL,
		AuthSchemes:    extractAuthSchemesV3(d),
	}, nil
}

func extractAuthSchemesV3(d *v3high.Document) []AuthScheme {
	if d.Components == nil || d.Components.SecuritySchemes == nil {
		return nil
	}
	var schemes []AuthScheme
	for key, s := range d.Components.SecuritySchemes.FromOldest() {
		switch s.Type {
		case "apiKey":
			scheme := AuthScheme{
				Kind:        "apiKey",
				ConfigVar:   lowercaseFirst(key),
				Description: s.Description,
				Secret:      true,
			}
			if s.In == "header" {
				scheme.HeaderName = s.Name
			} else if s.In == "query" {
				scheme.QueryParam = s.Name
			}
			schemes = append(schemes, scheme)
		case "http":
			switch strings.ToLower(s.Scheme) {
			case "bearer":
				schemes = append(schemes, AuthScheme{
					Kind:        "bearer",
					ConfigVar:   "bearerToken",
					HeaderName:  "Authorization",
					Description: s.Description,
					Secret:      true,
				})
			case "basic":
				schemes = append(schemes, AuthScheme{
					Kind:        "basic",
					Description: s.Description,
				})
			}
		case "oauth2", "openIdConnect":
			schemes = append(schemes, AuthScheme{
				Kind:        "bearer",
				ConfigVar:   "bearerToken",
				HeaderName:  "Authorization",
				Description: s.Description,
				Secret:      true,
			})
		}
	}
	return schemes
}

func extractBaseURLV3(d *v3high.Document) string {
	if len(d.Servers) > 0 && d.Servers[0].URL != "" {
		return d.Servers[0].URL
	}
	return ""
}

func buildResourceV3(g pathGroup, d *v3high.Document, pkgName string, rootTags map[string]bool, tc *typeCollectorV3) (ResourceDef, bool) {
	if d.Paths == nil || d.Paths.PathItems == nil {
		return ResourceDef{}, false
	}

	var collectionItem *v3high.PathItem
	var itemItem *v3high.PathItem

	if g.collectionPath != "" {
		if pi, ok := d.Paths.PathItems.Get(g.collectionPath); ok {
			collectionItem = pi
		}
	}
	if g.itemPath != "" {
		if pi, ok := d.Paths.PathItems.Get(g.itemPath); ok {
			itemItem = pi
		}
	}

	var createOp, readOp, updateOp, deleteOp *v3high.Operation
	var updateMethod string

	if collectionItem != nil && collectionItem.Post != nil {
		createOp = collectionItem.Post
	}
	if itemItem != nil {
		if itemItem.Get != nil {
			readOp = itemItem.Get
		}
		if itemItem.Put != nil {
			updateOp = itemItem.Put
			updateMethod = "PUT"
		} else if itemItem.Patch != nil {
			updateOp = itemItem.Patch
			updateMethod = "PATCH"
		}
		if itemItem.Delete != nil {
			deleteOp = itemItem.Delete
		}
	}

	if createOp == nil || (readOp == nil && deleteOp == nil) {
		return ResourceDef{}, false
	}

	module := moduleFromOps(rootTags, v3OpTags(createOp), v3OpTags(readOp), v3OpTags(updateOp), v3OpTags(deleteOp))
	token := fmt.Sprintf("%s:%s:%s", pkgName, module, g.name)
	idPathParam := g.idPathParam

	createSchema := requestBodySchemaV3(createOp, d.Components)
	readSchema := responseSchemaV3(readOp, d.Components)
	if readSchema == nil {
		readSchema = responseSchemaV3(createOp, d.Components)
	}

	inputs := map[string]pschema.PropertySpec{}
	outputs := map[string]pschema.PropertySpec{}
	apiPropertyNames := map[string]string{}
	var requiredInputs []string

	if createSchema != nil {
		if createSchema.Properties != nil {
			for apiName, propProxy := range createSchema.Properties.FromOldest() {
				camelName := toCamelCase(apiName)
				spec := tc.convertProperty(propProxy)
				inputs[camelName] = spec
				outputs[camelName] = spec
				apiPropertyNames[camelName] = apiName
			}
		}
		for _, r := range filterRequired(createSchema.Required, idPathParam) {
			requiredInputs = append(requiredInputs, toCamelCase(r))
		}
	}

	if readSchema != nil && readSchema.Properties != nil {
		for apiName, propProxy := range readSchema.Properties.FromOldest() {
			camelName := toCamelCase(apiName)
			if _, alreadyInput := inputs[camelName]; !alreadyInput {
				outputs[camelName] = tc.convertProperty(propProxy)
				apiPropertyNames[camelName] = apiName
			}
		}
	}

	idField := idPathParam
	if readSchema != nil && readSchema.Properties != nil {
		if _, ok := readSchema.Properties.Get("id"); ok {
			idField = "id"
		}
	}
	if idField != "" {
		delete(inputs, idField)
		delete(outputs, idField)
		delete(inputs, toCamelCase(idField))
		delete(outputs, toCamelCase(idField))
		requiredInputs = removeFromSlice(requiredInputs, idField)
		requiredInputs = removeFromSlice(requiredInputs, toCamelCase(idField))
		requiredInputs = removeFromSlice(requiredInputs, idPathParam)
		requiredInputs = removeFromSlice(requiredInputs, toCamelCase(idPathParam))
	}
	delete(outputs, "id")

	for _, ctxParam := range contextPathParams(g.itemPath, idPathParam) {
		if _, exists := inputs[ctxParam]; !exists {
			inputs[ctxParam] = pschema.PropertySpec{TypeSpec: pschema.TypeSpec{Type: "string"}}
			outputs[ctxParam] = pschema.PropertySpec{TypeSpec: pschema.TypeSpec{Type: "string"}}
			requiredInputs = append(requiredInputs, ctxParam)
		}
	}

	updatePath := ""
	if updateOp != nil {
		updatePath = g.itemPath
	}

	return ResourceDef{
		Name:             g.name,
		Token:            token,
		CreatePath:       g.collectionPath,
		CreateMethod:     "POST",
		ReadPath:         g.itemPath,
		UpdatePath:       updatePath,
		UpdateMethod:     updateMethod,
		DeletePath:       g.itemPath,
		IDPathParam:      idPathParam,
		IDField:          idField,
		InputSchema:      inputs,
		OutputSchema:     outputs,
		RequiredInputs:   requiredInputs,
		APIPropertyNames: apiPropertyNames,
	}, true
}

// requestBodySchemaV3 extracts the schema from an operation's application/json request body.
func requestBodySchemaV3(op *v3high.Operation, components *v3high.Components) *highbase.Schema {
	if op == nil || op.RequestBody == nil || op.RequestBody.Content == nil {
		return nil
	}
	if mt, ok := op.RequestBody.Content.Get("application/json"); ok && mt.Schema != nil {
		return resolveSchemaV3(mt.Schema, components)
	}
	return nil
}

// responseSchemaV3 extracts the schema from the first 2xx response's application/json content.
func responseSchemaV3(op *v3high.Operation, components *v3high.Components) *highbase.Schema {
	if op == nil || op.Responses == nil || op.Responses.Codes == nil {
		return nil
	}
	for _, code := range []string{"200", "201", "202"} {
		if resp, ok := op.Responses.Codes.Get(code); ok && resp.Content != nil {
			if mt, ok := resp.Content.Get("application/json"); ok && mt.Schema != nil {
				return resolveSchemaV3(mt.Schema, components)
			}
		}
	}
	return nil
}

// resolveSchemaV3 dereferences a SchemaProxy for OAS3, resolving #/components/schemas/ refs.
func resolveSchemaV3(proxy *highbase.SchemaProxy, components *v3high.Components) *highbase.Schema {
	if proxy == nil {
		return nil
	}
	ref := proxy.GetReference()
	if ref != "" && components != nil && components.Schemas != nil {
		if name := extractComponentSchemaName(ref); name != "" {
			if defProxy, ok := components.Schemas.Get(name); ok {
				return defProxy.Schema()
			}
		}
	}
	return proxy.Schema()
}

func extractComponentSchemaName(ref string) string {
	const prefix = "#/components/schemas/"
	if strings.HasPrefix(ref, prefix) {
		return ref[len(prefix):]
	}
	return ""
}

// typeCollectorV3 resolves and accumulates OAS3 component schemas as Pulumi types.
type typeCollectorV3 struct {
	pkgName    string
	components *v3high.Components
	types      map[string]pschema.ComplexTypeSpec
}

func (tc *typeCollectorV3) convertProperty(proxy *highbase.SchemaProxy) pschema.PropertySpec {
	if proxy == nil {
		return pschema.PropertySpec{TypeSpec: pschema.TypeSpec{Type: "string"}}
	}
	ref := proxy.GetReference()
	if ref != "" {
		if name := extractComponentSchemaName(ref); name != "" {
			tc.ensureType(name)
			return pschema.PropertySpec{
				TypeSpec: pschema.TypeSpec{
					Ref: fmt.Sprintf("#/types/%s:index:%s", tc.pkgName, toPascalCase(name)),
				},
			}
		}
	}
	return tc.convertSchema(proxy.Schema())
}

func (tc *typeCollectorV3) convertSchema(schema *highbase.Schema) pschema.PropertySpec {
	if schema == nil {
		return pschema.PropertySpec{TypeSpec: pschema.TypeSpec{Type: "string"}}
	}
	t := ""
	if len(schema.Type) > 0 {
		t = schema.Type[0]
	}
	switch t {
	case "integer":
		return pschema.PropertySpec{TypeSpec: pschema.TypeSpec{Type: "integer"}, Description: schema.Description}
	case "number":
		return pschema.PropertySpec{TypeSpec: pschema.TypeSpec{Type: "number"}, Description: schema.Description}
	case "boolean":
		return pschema.PropertySpec{TypeSpec: pschema.TypeSpec{Type: "boolean"}, Description: schema.Description}
	case "array":
		itemSpec := tc.arrayItemSpec(schema)
		return pschema.PropertySpec{
			TypeSpec:    pschema.TypeSpec{Type: "array", Items: &itemSpec},
			Description: schema.Description,
		}
	case "object":
		return pschema.PropertySpec{TypeSpec: pschema.TypeSpec{Type: "object"}, Description: schema.Description}
	default:
		return pschema.PropertySpec{TypeSpec: pschema.TypeSpec{Type: "string"}, Description: schema.Description}
	}
}

func (tc *typeCollectorV3) arrayItemSpec(schema *highbase.Schema) pschema.TypeSpec {
	if schema.Items == nil {
		return pschema.TypeSpec{Type: "string"}
	}
	itemProxy := schema.Items.A
	if itemProxy == nil {
		return pschema.TypeSpec{Type: "string"}
	}
	if ref := itemProxy.GetReference(); ref != "" {
		if name := extractComponentSchemaName(ref); name != "" {
			tc.ensureType(name)
			return pschema.TypeSpec{Ref: fmt.Sprintf("#/types/%s:index:%s", tc.pkgName, toPascalCase(name))}
		}
	}
	if s := itemProxy.Schema(); s != nil && len(s.Type) > 0 {
		return pschema.TypeSpec{Type: s.Type[0]}
	}
	return pschema.TypeSpec{Type: "string"}
}

func (tc *typeCollectorV3) ensureType(schemaName string) {
	if tc.components == nil || tc.components.Schemas == nil {
		return
	}
	token := fmt.Sprintf("%s:index:%s", tc.pkgName, toPascalCase(schemaName))
	if _, exists := tc.types[token]; exists {
		return
	}
	proxy, ok := tc.components.Schemas.Get(schemaName)
	if !ok {
		return
	}
	schema := proxy.Schema()
	if schema == nil {
		return
	}
	props := map[string]pschema.PropertySpec{}
	if schema.Properties != nil {
		for name, propProxy := range schema.Properties.FromOldest() {
			props[toCamelCase(name)] = tc.convertProperty(propProxy)
		}
	}
	tc.types[token] = pschema.ComplexTypeSpec{
		ObjectTypeSpec: pschema.ObjectTypeSpec{
			Type:        "object",
			Description: schema.Description,
			Properties:  props,
			Required:    schema.Required,
		},
	}
}
