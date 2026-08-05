// Command preflight reports what pulumi-openapi-provider will discover from an
// OpenAPI/Swagger spec, without generating an SDK or contacting the API.
//
// It calls the library's own spec.Discover and spec.BuildSchema, so its output is
// what the provider will actually do — not a re-implementation of the rules.
//
//	go run . <spec-url-or-file> [pkgname]
//
// Reported: discovered resources (paths, ID param/field, inputs, required inputs),
// spec paths that no resource claimed, enum values dropped from the schema, the
// derived provider config variables, and any auth scheme that is discovered but
// not actually sent at runtime.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/pb33f/libopenapi"
	pschema "github.com/pulumi/pulumi/pkg/v3/codegen/schema"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/pkg/spec"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run . <spec-url-or-file> [pkgname]")
		os.Exit(2)
	}
	src := os.Args[1]
	pkgName := "preflight"
	if len(os.Args) > 2 {
		pkgName = os.Args[2]
	}

	doc, err := spec.LoadSpec(src, "preflight/1.0")
	if err != nil {
		fmt.Fprintf(os.Stderr, "loading spec: %v\n", err)
		os.Exit(1)
	}

	result, err := spec.Discover(doc, pkgName, nil, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discovering resources: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("spec format: %s\n", doc.GetSpecInfo().SpecFormat)
	fmt.Printf("base URL:    %q", result.DefaultBaseURL)
	if result.DefaultBaseURL == "" {
		fmt.Print("   <- EMPTY: supply --base-url / Options.BaseURL / config baseUrl, or every call fails")
	}
	fmt.Printf("\n\n")

	// ---- resources ---------------------------------------------------------
	methods := specMethods(doc)
	fmt.Printf("RESOURCES (%d)\n", len(result.Resources))
	claimed := map[string]bool{}
	for _, r := range sortedResources(result.Resources) {
		claimed[normalise(r.CreatePath)] = true
		claimed[normalise(r.ReadPath)] = true
		claimed[normalise(r.DeletePath)] = true
		if r.UpdatePath != "" {
			claimed[normalise(r.UpdatePath)] = true
		}

		fmt.Printf("\n  %s  (token %s)\n", r.Name, r.Token)
		fmt.Printf("    create  POST   %s\n", r.CreatePath)
		fmt.Printf("    read    GET    %s\n", r.ReadPath)
		if r.UpdatePath != "" {
			fmt.Printf("    update  %-6s %s\n", r.UpdateMethod, r.UpdatePath)
		} else {
			fmt.Printf("    update  <none>  -- Pulumi will replace on change\n")
			for _, c := range updateCandidates(methods, r) {
				fmt.Printf("            CANDIDATE %s -- not on the item path, so discovery ignored it; "+
					"ResourceOverride{UpdatePath, UpdateMethod} can wire it up\n", c)
			}
		}
		fmt.Printf("    delete  DELETE %s\n", r.DeletePath)
		fmt.Printf("    id      path param %q, extracted from create response field %q\n", r.IDPathParam, r.IDField)
		fmt.Printf("    inputs  %s\n", strings.Join(sortedKeys(r.InputSchema), ", "))
		if len(r.RequiredInputs) > 0 {
			fmt.Printf("    required %s\n", strings.Join(r.RequiredInputs, ", "))
		}
		if r.Deprecated {
			fmt.Printf("    NOTE: create operation is marked deprecated in the spec\n")
		}
	}

	// ---- unclaimed paths ---------------------------------------------------
	var unclaimed []string
	for _, p := range specPaths(doc) {
		if !claimed[normalise(p)] {
			unclaimed = append(unclaimed, p)
		}
	}
	fmt.Printf("\n\nPATHS NOT MAPPED TO ANY RESOURCE (%d)\n", len(unclaimed))
	fmt.Println("  (expected for list/search/action endpoints; a surprise here means a resource was dropped)")
	for _, p := range unclaimed {
		fmt.Printf("  %-40s %s\n", p, strings.Join(methods[normalise(p)], " "))
	}

	// ---- auth + config -----------------------------------------------------
	fmt.Printf("\n\nAUTH SCHEMES (%d)\n", len(result.AuthSchemes))
	for _, s := range result.AuthSchemes {
		switch {
		case s.QueryParam != "":
			fmt.Printf("  %-12s query param %q -- NOT SENT AT RUNTIME: calls go out unauthenticated\n", s.Kind, s.QueryParam)
		case s.Kind == "basic":
			fmt.Printf("  basic        config vars username/password -> Authorization: Basic <base64>\n")
		default:
			fmt.Printf("  %-12s config var %q -> header %q\n", s.Kind, s.ConfigVar, s.HeaderName)
		}
	}
	if len(result.AuthSchemes) == 0 {
		fmt.Println("  none declared -- falls back to generic apiKey / apiKeyHeader / bearerToken config vars")
	}

	schemaJSON, err := spec.BuildSchema(pkgName, "0.1.0", result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "building schema: %v\n", err)
		os.Exit(1)
	}
	var pkg pschema.PackageSpec
	if err := json.Unmarshal([]byte(schemaJSON), &pkg); err != nil {
		fmt.Fprintf(os.Stderr, "decoding schema: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n\nPROVIDER CONFIG (pulumi config set %s:<var>)\n", pkg.Name)
	for _, k := range sortedKeys(pkg.Config.Variables) {
		v := pkg.Config.Variables[k]
		secret := ""
		if v.Secret {
			secret = "  (secret)"
		}
		fmt.Printf("  %s%s\n", k, secret)
	}

	// ---- enums -------------------------------------------------------------
	fmt.Printf("\n\nENUM TYPES (%d of %d types)\n", countEnums(pkg.Types), len(pkg.Types))
	for _, tok := range sortedTypeKeys(pkg.Types) {
		t := pkg.Types[tok]
		if len(t.Enum) == 0 {
			continue
		}
		vals := make([]string, 0, len(t.Enum))
		for _, e := range t.Enum {
			vals = append(vals, fmt.Sprintf("%v", e.Value))
		}
		fmt.Printf("  %-40s %s\n", tok, strings.Join(vals, " | "))
	}
	fmt.Println("  NOTE: empty-string and null enum values are dropped from the SDK -- compare against the spec")
}

func normalise(p string) string { return strings.TrimSuffix(p, "/") }

// updateCandidates reports endpoints that look like an update but sit somewhere
// discovery does not look: PUT/PATCH on the collection path (update by body ID)
// and POST on the item path (the Swagger Petstore's form-encoded update).
func updateCandidates(methods map[string][]string, r spec.ResourceDef) []string {
	var out []string
	for _, m := range methods[normalise(r.CreatePath)] {
		if m == "PUT" || m == "PATCH" {
			out = append(out, m+" "+r.CreatePath)
		}
	}
	for _, m := range methods[normalise(r.ReadPath)] {
		if m == "POST" {
			out = append(out, m+" "+r.ReadPath)
		}
	}
	return out
}

// specMethods maps each spec path to the HTTP methods it declares.
func specMethods(doc libopenapi.Document) map[string][]string {
	out := map[string][]string{}
	if doc.GetSpecInfo().SpecFormat == "oas2" {
		model, err := doc.BuildV2Model()
		if err != nil || model == nil || model.Model.Paths == nil {
			return out
		}
		for pair := model.Model.Paths.PathItems.Oldest(); pair != nil; pair = pair.Next() {
			pi := pair.Value
			var ms []string
			for _, m := range []struct {
				name string
				set  bool
			}{
				{"GET", pi.Get != nil}, {"POST", pi.Post != nil}, {"PUT", pi.Put != nil},
				{"PATCH", pi.Patch != nil}, {"DELETE", pi.Delete != nil},
			} {
				if m.set {
					ms = append(ms, m.name)
				}
			}
			out[normalise(pair.Key)] = ms
		}
		return out
	}
	model, err := doc.BuildV3Model()
	if err != nil || model == nil || model.Model.Paths == nil {
		return out
	}
	for k, pi := range model.Model.Paths.PathItems.FromOldest() {
		var ms []string
		for _, m := range []struct {
			name string
			set  bool
		}{
			{"GET", pi.Get != nil}, {"POST", pi.Post != nil}, {"PUT", pi.Put != nil},
			{"PATCH", pi.Patch != nil}, {"DELETE", pi.Delete != nil},
		} {
			if m.set {
				ms = append(ms, m.name)
			}
		}
		out[normalise(k)] = ms
	}
	return out
}

func specPaths(doc libopenapi.Document) []string {
	var out []string
	if doc.GetSpecInfo().SpecFormat == "oas2" {
		model, err := doc.BuildV2Model()
		if err != nil || model == nil || model.Model.Paths == nil {
			return out
		}
		for pair := model.Model.Paths.PathItems.Oldest(); pair != nil; pair = pair.Next() {
			out = append(out, pair.Key)
		}
		return out
	}
	model, err := doc.BuildV3Model()
	if err != nil || model == nil || model.Model.Paths == nil {
		return out
	}
	for k := range model.Model.Paths.PathItems.FromOldest() {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedResources(in []spec.ResourceDef) []spec.ResourceDef {
	out := make([]spec.ResourceDef, len(in))
	copy(out, in)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedTypeKeys(m map[string]pschema.ComplexTypeSpec) []string { return sortedKeys(m) }

func countEnums(m map[string]pschema.ComplexTypeSpec) int {
	n := 0
	for _, t := range m {
		if len(t.Enum) > 0 {
			n++
		}
	}
	return n
}
