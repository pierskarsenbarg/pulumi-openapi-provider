package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	p "github.com/pulumi/pulumi-go-provider"
	"github.com/pulumi/pulumi/sdk/v3/go/property"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/config"
	"github.com/pierskarsenbarg/pulumi-openapi-provider/spec"
)

// --- substituteAllParams ---

func TestSubstituteAllParams_IDOnly(t *testing.T) {
	got := substituteAllParams("/pets/{petId}", "42", "petId", nil)
	if got != "/pets/42" {
		t.Errorf("got %q, want /pets/42", got)
	}
}

func TestSubstituteAllParams_ContextParam(t *testing.T) {
	vals := map[string]any{"orgName": "acme"}
	got := substituteAllParams("/orgs/{orgName}/tokens/{tokenId}", "99", "tokenId", vals)
	if got != "/orgs/acme/tokens/99" {
		t.Errorf("got %q, want /orgs/acme/tokens/99", got)
	}
}

func TestSubstituteAllParams_MissingContextParam(t *testing.T) {
	got := substituteAllParams("/orgs/{orgName}/tokens/{tokenId}", "1", "tokenId", nil)
	if got != "/orgs//tokens/1" {
		t.Errorf("got %q, want /orgs//tokens/1", got)
	}
}

func TestSubstituteAllParams_NoID(t *testing.T) {
	// when id is empty the idParam placeholder is not replaced by the id path, but the
	// remaining-placeholders loop still runs and substitutes it with an empty string
	got := substituteAllParams("/pets/{petId}", "", "petId", nil)
	if got != "/pets/" {
		t.Errorf("got %q, want /pets/", got)
	}
}

func TestSubstituteAllParams_NoParams(t *testing.T) {
	got := substituteAllParams("/pets", "", "", nil)
	if got != "/pets" {
		t.Errorf("got %q, want /pets", got)
	}
}

// --- extractID ---

func TestExtractID_ByIDField(t *testing.T) {
	body := map[string]any{"petId": "7", "name": "Fido"}
	got := extractID(body, "petId", "petId")
	if got != "7" {
		t.Errorf("got %q, want 7", got)
	}
}

func TestExtractID_FallsBackToIDPathParam(t *testing.T) {
	body := map[string]any{"petId": "8"}
	got := extractID(body, "id", "petId")
	if got != "8" {
		t.Errorf("got %q, want 8", got)
	}
}

func TestExtractID_FallsBackToId(t *testing.T) {
	body := map[string]any{"id": "9"}
	got := extractID(body, "", "")
	if got != "9" {
		t.Errorf("got %q, want 9", got)
	}
}

func TestExtractID_NotFound(t *testing.T) {
	got := extractID(map[string]any{"name": "Fido"}, "petId", "petId")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestExtractID_DotNotation(t *testing.T) {
	body := map[string]any{
		"metadata": map[string]any{"name": "my-namespace"},
	}
	got := extractID(body, "metadata.name", "name")
	if got != "my-namespace" {
		t.Errorf("got %q, want my-namespace", got)
	}
}

func TestExtractID_DotNotationDeep(t *testing.T) {
	body := map[string]any{
		"a": map[string]any{
			"b": map[string]any{"c": "deep-value"},
		},
	}
	got := extractID(body, "a.b.c", "")
	if got != "deep-value" {
		t.Errorf("got %q, want deep-value", got)
	}
}

func TestExtractID_DotNotationMissingKey(t *testing.T) {
	body := map[string]any{
		"metadata": map[string]any{"uid": "abc"},
	}
	got := extractID(body, "metadata.name", "")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

func TestExtractID_DotNotationNonMapIntermediate(t *testing.T) {
	body := map[string]any{"metadata": "not-a-map"}
	got := extractID(body, "metadata.name", "")
	if got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

// --- propertyMapToGoMap / goMapToPropertyMap ---

func TestPropertyMapRoundTrip(t *testing.T) {
	original := map[string]any{
		"name":   "Fido",
		"age":    float64(3),
		"active": true,
	}
	pm := goMapToPropertyMap(original)
	got := propertyMapToGoMap(pm)

	for k, want := range original {
		if got[k] != want {
			t.Errorf("key %q: got %v, want %v", k, got[k], want)
		}
	}
}

func TestGoValueToProperty_Types(t *testing.T) {
	cases := []struct {
		input any
		check func(property.Value) bool
	}{
		{nil, func(v property.Value) bool { return v.IsNull() }},
		{true, func(v property.Value) bool { return v.IsBool() && v.AsBool() }},
		{float64(3.14), func(v property.Value) bool { return v.IsNumber() && v.AsNumber() == 3.14 }},
		{"hello", func(v property.Value) bool { return v.IsString() && v.AsString() == "hello" }},
		{[]any{"a", "b"}, func(v property.Value) bool { return v.IsArray() && v.AsArray().Len() == 2 }},
		{map[string]any{"k": "v"}, func(v property.Value) bool { return v.IsMap() }},
	}
	for _, tc := range cases {
		v := goValueToProperty(tc.input)
		if !tc.check(v) {
			t.Errorf("goValueToProperty(%v) produced unexpected value %v", tc.input, v)
		}
	}
}

func TestPropertyValueToGo_Array(t *testing.T) {
	arr := property.New(property.NewArray([]property.Value{
		property.New("x"),
		property.New("y"),
	}))
	got := propertyValueToGo(arr)
	slice, ok := got.([]any)
	if !ok || len(slice) != 2 {
		t.Errorf("expected []any of len 2, got %T %v", got, got)
	}
}

func TestPropertyValueToGo_Null(t *testing.T) {
	got := propertyValueToGo(property.New(property.Null))
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

// --- mergeInputsIntoOutputs ---

func TestMergeInputsIntoOutputs_OutputsTakePrecedence(t *testing.T) {
	inputs := property.NewMap(map[string]property.Value{
		"name":  property.New("from-input"),
		"extra": property.New("only-in-input"),
	})
	outputs := property.NewMap(map[string]property.Value{
		"name": property.New("from-output"),
	})
	merged := mergeInputsIntoOutputs(inputs, outputs)
	if v, _ := merged.GetOk("name"); v.AsString() != "from-output" {
		t.Errorf("name = %q, want from-output", v.AsString())
	}
	if v, _ := merged.GetOk("extra"); v.AsString() != "only-in-input" {
		t.Errorf("extra = %q, want only-in-input", v.AsString())
	}
}

// --- setID ---

func TestSetID(t *testing.T) {
	m := property.NewMap(map[string]property.Value{"name": property.New("Fido")})
	m = setID(m, "petId", "42")
	v, ok := m.GetOk("petId")
	if !ok || v.AsString() != "42" {
		t.Errorf("petId = %v (ok=%v), want 42", v, ok)
	}
}

func TestSetID_EmptyField(t *testing.T) {
	m := property.NewMap(map[string]property.Value{"name": property.New("Fido")})
	m2 := setID(m, "", "42")
	if _, ok := m2.GetOk(""); ok {
		t.Error("should not set empty key")
	}
}

// --- isNotFound / notFoundError ---

func TestIsNotFound(t *testing.T) {
	if !isNotFound(&notFoundError{status: 404}) {
		t.Error("expected isNotFound to return true for 404")
	}
	if isNotFound(nil) {
		t.Error("expected isNotFound to return false for nil")
	}
	if isNotFound(&notFoundError{status: 500}) {
		t.Error("expected isNotFound to return false for 500")
	}
}

func TestNotFoundError_Error(t *testing.T) {
	e := &notFoundError{status: 404}
	if e.Error() != "HTTP 404" {
		t.Errorf("Error() = %q, want HTTP 404", e.Error())
	}
}

// --- computeDiff ---

func TestComputeDiff_NoChanges(t *testing.T) {
	state := property.NewMap(map[string]property.Value{"name": property.New("Fido")})
	inputs := property.NewMap(map[string]property.Value{"name": property.New("Fido")})
	resp, err := computeDiff(context.Background(), p.DiffRequest{State: state, Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	if resp.HasChanges {
		t.Error("expected no changes")
	}
}

func TestComputeDiff_Update(t *testing.T) {
	state := property.NewMap(map[string]property.Value{"name": property.New("Fido")})
	inputs := property.NewMap(map[string]property.Value{"name": property.New("Rex")})
	resp, _ := computeDiff(context.Background(), p.DiffRequest{State: state, Inputs: inputs})
	if !resp.HasChanges {
		t.Error("expected changes")
	}
	if resp.DetailedDiff["name"].Kind != p.Update {
		t.Errorf("expected Update diff for name, got %v", resp.DetailedDiff["name"].Kind)
	}
}

func TestComputeDiff_Delete(t *testing.T) {
	state := property.NewMap(map[string]property.Value{"name": property.New("Fido"), "old": property.New("gone")})
	inputs := property.NewMap(map[string]property.Value{"name": property.New("Fido")})
	resp, _ := computeDiff(context.Background(), p.DiffRequest{State: state, Inputs: inputs})
	if !resp.HasChanges {
		t.Error("expected changes")
	}
	if resp.DetailedDiff["old"].Kind != p.Delete {
		t.Errorf("expected Delete diff for old, got %v", resp.DetailedDiff["old"].Kind)
	}
}

func TestComputeDiff_Add(t *testing.T) {
	state := property.NewMap(map[string]property.Value{})
	inputs := property.NewMap(map[string]property.Value{"name": property.New("Fido")})
	resp, _ := computeDiff(context.Background(), p.DiffRequest{State: state, Inputs: inputs})
	if !resp.HasChanges {
		t.Error("expected changes")
	}
}

// --- tokenFromURN ---

func TestTokenFromURN(t *testing.T) {
	urn := "urn:pulumi:stack::project::mypkg:index:Pet::myPet"
	got := tokenFromURN(urn)
	if got != "mypkg:index:Pet" {
		t.Errorf("got %q, want mypkg:index:Pet", got)
	}
}

func TestTokenFromURN_Short(t *testing.T) {
	got := tokenFromURN("short")
	if got != "short" {
		t.Errorf("got %q, want short (passthrough)", got)
	}
}

// --- CRUD via httptest ---

func testServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *config.ProviderConfig) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	cfg := config.New(nil, srv.URL, nil, "", nil)
	return srv, cfg
}

func testResource() spec.ResourceDef {
	return spec.ResourceDef{
		Name:         "Widget",
		Token:        "test:index:Widget",
		CreatePath:   "/widgets",
		CreateMethod: "POST",
		ReadPath:     "/widgets/{widgetId}",
		DeletePath:   "/widgets/{widgetId}",
		IDPathParam:  "widgetId",
		IDField:      "widgetId",
	}
}

func TestCRUD_Create(t *testing.T) {
	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/widgets" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"widgetId": "42", "name": "Foo"})
	})

	client := &crudClient{cfg: cfg}
	inputs := property.NewMap(map[string]property.Value{"name": property.New("Foo")})
	id, outputs, err := client.create(context.Background(), testResource(), inputs)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id != "42" {
		t.Errorf("id = %q, want 42", id)
	}
	if v, _ := outputs.GetOk("name"); v.AsString() != "Foo" {
		t.Errorf("name = %q, want Foo", v.AsString())
	}
}

func TestCRUD_Create_MissingID(t *testing.T) {
	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"name": "Foo"})
	})

	client := &crudClient{cfg: cfg}
	inputs := property.NewMap(map[string]property.Value{"name": property.New("Foo")})
	_, _, err := client.create(context.Background(), testResource(), inputs)
	if err == nil {
		t.Fatal("expected error when ID is missing from response")
	}
}

func TestCRUD_Read(t *testing.T) {
	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/widgets/42" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"widgetId": "42", "name": "Foo"})
	})

	client := &crudClient{cfg: cfg}
	outputs, err := client.read(context.Background(), testResource(), "42", nil)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if v, _ := outputs.GetOk("name"); v.AsString() != "Foo" {
		t.Errorf("name = %q, want Foo", v.AsString())
	}
}

func TestCRUD_Read_NotFound(t *testing.T) {
	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	client := &crudClient{cfg: cfg}
	outputs, err := client.read(context.Background(), testResource(), "99", nil)
	if err != nil {
		t.Fatalf("expected nil error on 404, got %v", err)
	}
	if outputs.Len() != 0 {
		t.Error("expected empty outputs on 404")
	}
}

func TestCRUD_Update(t *testing.T) {
	res := testResource()
	res.UpdatePath = "/widgets/{widgetId}"
	res.UpdateMethod = "PUT"

	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/widgets/42" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"widgetId": "42", "name": "Bar"})
	})

	client := &crudClient{cfg: cfg}
	inputs := property.NewMap(map[string]property.Value{"name": property.New("Bar")})
	outputs, err := client.update(context.Background(), res, "42", inputs)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if v, _ := outputs.GetOk("name"); v.AsString() != "Bar" {
		t.Errorf("name = %q, want Bar", v.AsString())
	}
}

func TestCRUD_Update_NoUpdatePath(t *testing.T) {
	res := testResource() // UpdatePath is empty — no HTTP call should be made
	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("unexpected HTTP call when UpdatePath is empty")
	})

	client := &crudClient{cfg: cfg}
	inputs := property.NewMap(map[string]property.Value{"name": property.New("Foo")})
	outputs, err := client.update(context.Background(), res, "42", inputs)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if v, _ := outputs.GetOk("name"); v.AsString() != "Foo" {
		t.Errorf("name = %q, want Foo", v.AsString())
	}
}

func TestCRUD_Delete(t *testing.T) {
	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/widgets/42" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	client := &crudClient{cfg: cfg}
	if err := client.del(context.Background(), testResource(), "42", nil); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestCRUD_Delete_NotFound_IsOK(t *testing.T) {
	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	client := &crudClient{cfg: cfg}
	if err := client.del(context.Background(), testResource(), "99", nil); err != nil {
		t.Fatalf("expected 404 on delete to be ignored, got: %v", err)
	}
}

func TestCRUD_Create_EmptyBaseURL(t *testing.T) {
	cfg := config.New(nil, "", nil, "", nil)
	client := &crudClient{cfg: cfg}
	inputs := property.NewMap(map[string]property.Value{"name": property.New("Foo")})
	_, _, err := client.create(context.Background(), testResource(), inputs)
	if err == nil {
		t.Fatal("expected error when baseUrl is empty")
	}
	if !strings.Contains(err.Error(), "baseUrl is not set") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestCRUD_Read_EmptyBaseURL(t *testing.T) {
	cfg := config.New(nil, "", nil, "", nil)
	client := &crudClient{cfg: cfg}
	_, err := client.read(context.Background(), testResource(), "42", nil)
	if err == nil {
		t.Fatal("expected error when baseUrl is empty")
	}
	if !strings.Contains(err.Error(), "baseUrl is not set") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestDo_NonSuccessStatus(t *testing.T) {
	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal error"))
	})

	client := &crudClient{cfg: cfg}
	_, err := client.read(context.Background(), testResource(), "1", nil)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestDo_EmptyBody(t *testing.T) {
	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	client := &crudClient{cfg: cfg}
	if err := client.del(context.Background(), testResource(), "1", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- handleCheck ---

func TestHandleCheck_NoRequiredInputs(t *testing.T) {
	res := testResource() // RequiredInputs is nil
	inputs := property.NewMap(map[string]property.Value{"name": property.New("Fido")})
	resp, err := handleCheck(context.Background(), res, p.CheckRequest{Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Failures) != 0 {
		t.Errorf("expected no failures, got %v", resp.Failures)
	}
}

func TestHandleCheck_AllRequiredPresent(t *testing.T) {
	res := testResource()
	res.RequiredInputs = []string{"name", "status"}
	inputs := property.NewMap(map[string]property.Value{
		"name":   property.New("Fido"),
		"status": property.New("available"),
	})
	resp, err := handleCheck(context.Background(), res, p.CheckRequest{Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Failures) != 0 {
		t.Errorf("expected no failures, got %v", resp.Failures)
	}
}

func TestHandleCheck_MissingRequired(t *testing.T) {
	res := testResource()
	res.RequiredInputs = []string{"name", "status"}
	inputs := property.NewMap(map[string]property.Value{
		"name": property.New("Fido"),
		// "status" is missing
	})
	resp, err := handleCheck(context.Background(), res, p.CheckRequest{Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Failures) != 1 {
		t.Fatalf("expected 1 failure, got %d: %v", len(resp.Failures), resp.Failures)
	}
	if resp.Failures[0].Property != "status" {
		t.Errorf("failure property = %q, want status", resp.Failures[0].Property)
	}
}

func TestHandleCheck_AllRequiredMissing(t *testing.T) {
	res := testResource()
	res.RequiredInputs = []string{"name", "status"}
	resp, err := handleCheck(context.Background(), res, p.CheckRequest{Inputs: property.NewMap(nil)})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Failures) != 2 {
		t.Fatalf("expected 2 failures, got %d: %v", len(resp.Failures), resp.Failures)
	}
}

func TestHandleCheck_InputsPassedThrough(t *testing.T) {
	res := testResource()
	res.RequiredInputs = []string{"name"}
	inputs := property.NewMap(map[string]property.Value{
		"name":  property.New("Fido"),
		"extra": property.New("bonus"),
	})
	resp, err := handleCheck(context.Background(), res, p.CheckRequest{Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := resp.Inputs.GetOk("extra"); !ok || v.AsString() != "bonus" {
		t.Error("inputs were not passed through unchanged")
	}
}

func TestHandleCheck_ContextParamRequired(t *testing.T) {
	res := testResource()
	res.RequiredInputs = []string{"orgName"}
	inputs := property.NewMap(map[string]property.Value{}) // orgName missing
	resp, err := handleCheck(context.Background(), res, p.CheckRequest{Inputs: inputs})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Failures) != 1 || resp.Failures[0].Property != "orgName" {
		t.Errorf("expected failure for orgName, got %v", resp.Failures)
	}
}

// --- Configure ---

func TestConfigure_OKWhenBaseURLFromConfig(t *testing.T) {
	cfg := config.New(nil, "", nil, "", nil)
	provider := Build("test", "0.0.0", spec.DiscoveryResult{}, cfg, false, PollingConfig{}, nil)
	args := property.NewMap(map[string]property.Value{
		"baseUrl": property.New("https://api.example.com"),
	})
	err := provider.Configure(context.Background(), p.ConfigureRequest{Args: args})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigure_OKWhenBaseURLFromDefault(t *testing.T) {
	cfg := config.New(nil, "https://api.example.com", nil, "", nil)
	provider := Build("test", "0.0.0", spec.DiscoveryResult{}, cfg, false, PollingConfig{}, nil)
	err := provider.Configure(context.Background(), p.ConfigureRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Polling ---

// fastPolling returns a PollingConfig with a short timeout and minimal intervals for tests.
func fastPolling(timeout time.Duration) PollingConfig {
	return ResolvePollingConfig(timeout, 10*time.Millisecond, 50*time.Millisecond, 1.5)
}

func TestPolling_WaitUntilExists_SucceedsOnSecondPoll(t *testing.T) {
	calls := 0
	res := testResource()
	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == http.MethodGet && calls < 2 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"widgetId": "42", "name": "Foo"})
	})

	client := &crudClient{cfg: cfg, pollingEnabled: true, polling: fastPolling(2 * time.Second)}
	err := client.waitUntilExists(context.Background(), res, "42", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls < 2 {
		t.Errorf("expected at least 2 GET calls, got %d", calls)
	}
}

func TestPolling_WaitUntilExists_TimesOut(t *testing.T) {
	res := testResource()
	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	client := &crudClient{cfg: cfg, pollingEnabled: true, polling: fastPolling(100 * time.Millisecond)}
	err := client.waitUntilExists(context.Background(), res, "42", map[string]any{})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestPolling_WaitUntilGone_SucceedsOnSecondPoll(t *testing.T) {
	calls := 0
	res := testResource()
	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method == http.MethodGet && calls < 2 {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"widgetId": "42"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})

	client := &crudClient{cfg: cfg, pollingEnabled: true, polling: fastPolling(2 * time.Second)}
	err := client.waitUntilGone(context.Background(), res, "42", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls < 2 {
		t.Errorf("expected at least 2 GET calls, got %d", calls)
	}
}

func TestPolling_WaitUntilGone_TimesOut(t *testing.T) {
	res := testResource()
	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"widgetId": "42"})
	})

	client := &crudClient{cfg: cfg, pollingEnabled: true, polling: fastPolling(100 * time.Millisecond)}
	err := client.waitUntilGone(context.Background(), res, "42", map[string]any{})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestPolling_ContextCancellation(t *testing.T) {
	res := testResource()
	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	ctx, cancel := context.WithCancel(context.Background())
	client := &crudClient{cfg: cfg, pollingEnabled: true, polling: fastPolling(10 * time.Second)}

	done := make(chan error, 1)
	go func() {
		done <- client.waitUntilExists(ctx, res, "42", map[string]any{})
	}()

	cancel()
	err := <-done
	if err == nil {
		t.Fatal("expected cancellation error, got nil")
	}
}

func TestPolling_DisabledSkipsPoll(t *testing.T) {
	getCalls := 0
	res := testResource()
	_, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"widgetId": "42", "name": "Foo"})
			return
		}
		getCalls++
		w.WriteHeader(http.StatusNotFound)
	})

	client := &crudClient{cfg: cfg, pollingEnabled: false, polling: fastPolling(2 * time.Second)}
	inputs := property.NewMap(map[string]property.Value{"name": property.New("Foo")})
	_, _, err := client.create(context.Background(), res, inputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if getCalls > 0 {
		t.Errorf("expected no GET calls with polling disabled, got %d", getCalls)
	}
}

// TestPolling_WaitUntilExists_WithContextParams verifies that context path params (e.g.
// {orgId}) supplied as inputs are available for URL substitution during polling even when
// the create response body omits them.
func TestPolling_WaitUntilExists_WithContextParams(t *testing.T) {
	// Resource nested under {orgId} — a context param the POST response won't include.
	res := spec.ResourceDef{
		Name:         "Item",
		Token:        "test:index:Item",
		CreatePath:   "/orgs/{orgId}/items",
		CreateMethod: "POST",
		ReadPath:     "/orgs/{orgId}/items/{itemId}",
		DeletePath:   "/orgs/{orgId}/items/{itemId}",
		IDPathParam:  "itemId",
		IDField:      "itemId",
	}

	getCalls := 0
	var capturedPaths []string
	srv, cfg := testServer(t, func(w http.ResponseWriter, r *http.Request) {
		capturedPaths = append(capturedPaths, r.URL.Path)
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			// Response deliberately omits orgId — mirrors real-world nested APIs.
			_ = json.NewEncoder(w).Encode(map[string]any{"itemId": "abc-123"})
			return
		}
		getCalls++
		if getCalls < 2 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"itemId": "abc-123", "name": "Widget"})
	})
	_ = srv

	client := &crudClient{cfg: cfg, pollingEnabled: true, polling: fastPolling(2 * time.Second)}
	inputs := property.NewMap(map[string]property.Value{
		"orgId": property.New("org-99"),
		"name":  property.New("Widget"),
	})
	id, outputs, err := client.create(context.Background(), res, inputs)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id != "abc-123" {
		t.Errorf("id = %q, want abc-123", id)
	}
	if v, _ := outputs.GetOk("name"); v.AsString() != "Widget" {
		t.Errorf("name = %q, want Widget", v.AsString())
	}
	// The GET paths must include the orgId substituted from inputs, not an empty segment.
	for _, path := range capturedPaths {
		if r := "/orgs//items"; strings.Contains(path, r) {
			t.Errorf("path %q contains empty org segment %q — orgId was not substituted", path, r)
		}
	}
	if getCalls < 2 {
		t.Errorf("expected at least 2 GET calls, got %d", getCalls)
	}
}
