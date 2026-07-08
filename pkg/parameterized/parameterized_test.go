package parameterized

import (
	"net/http"
	"net/http/httptest"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
)

func TestSlugifyTitle(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Petstore API", "petstore-api"},
		{"My Cool Service", "my-cool-service"},
		{"  leading/trailing spaces  ", "leading-trailing-spaces"},
		{"API v3.0", "api-v3-0"},
		{"", "openapi"},
		{"!!!###", "openapi"},
		{"hello---world", "hello-world"},
		{"UPPERCASE", "uppercase"},
		{"numbers123", "numbers123"},
	}
	for _, tc := range tests {
		got := slugifyTitle(tc.input)
		if got != tc.want {
			t.Errorf("slugifyTitle(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestNormalizeVersion(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"1.0.0", "1.0.0"},
		{"v1.0.0", "1.0.0"},
		{"1.0", "1.0.0"},
		{"1", "1.0.0"},
		{"2.3.4", "2.3.4"},
		{"2.0 beta", "2.0.0"},
		{"v3.5.1", "3.5.1"},
		{"", "1.0.0"},
		{"abc", "1.0.0"},
		{"0.0.1", "0.0.1"},
	}
	for _, tc := range tests {
		got := normalizeVersion(tc.input)
		if got != tc.want {
			t.Errorf("normalizeVersion(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestParameterize_AlwaysUsesDefaultUserAgent(t *testing.T) {
	var gotUserAgent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write([]byte(`swagger: "2.0"
info:
  title: Test API
  version: "1.0"
paths: {}
`))
	}))
	defer srv.Close()

	pp := &parameterizedProvider{binaryVersion: "9.9.9"}
	// --user-agent is not a recognised flag; it must not influence the resolved UA.
	_, err := pp.parameterize(t.Context(), p.ParameterizeRequest{
		Args: &p.ParameterizeRequestArgs{Args: []string{srv.URL, "--user-agent=should-be-ignored/1.0"}},
	})
	if err != nil {
		t.Fatalf("parameterize: %v", err)
	}
	if want := "pulumi-openapi-provider/9.9.9"; gotUserAgent != want {
		t.Errorf("User-Agent = %q, want %q", gotUserAgent, want)
	}
}

func TestParseParamArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantSpec string
		wantBase string
		wantErr  bool
	}{
		{
			name:     "https url only",
			args:     []string{"https://api.example.com/openapi.json"},
			wantSpec: "https://api.example.com/openapi.json",
		},
		{
			name:     "http url only",
			args:     []string{"http://localhost:3000/openapi.json"},
			wantSpec: "http://localhost:3000/openapi.json",
		},
		{
			name:     "absolute file path",
			args:     []string{"/path/to/spec.yaml"},
			wantSpec: "/path/to/spec.yaml",
		},
		{
			name:     "relative file path",
			args:     []string{"./spec.yaml"},
			wantSpec: "./spec.yaml",
		},
		{
			name:     "file:// uri",
			args:     []string{"file:///path/to/spec.yaml"},
			wantSpec: "file:///path/to/spec.yaml",
		},
		{
			name:     "url and --base-url= form",
			args:     []string{"https://api.example.com/openapi.json", "--base-url=https://api.example.com"},
			wantSpec: "https://api.example.com/openapi.json",
			wantBase: "https://api.example.com",
		},
		{
			name:     "url and --base-url space form",
			args:     []string{"https://api.example.com/openapi.json", "--base-url", "https://api.example.com"},
			wantSpec: "https://api.example.com/openapi.json",
			wantBase: "https://api.example.com",
		},
		{
			name:     "file path and --base-url",
			args:     []string{"/path/to/spec.yaml", "--base-url=https://api.example.com"},
			wantSpec: "/path/to/spec.yaml",
			wantBase: "https://api.example.com",
		},
		{
			name:    "empty args",
			args:    []string{},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseParamArgs(tc.args)
			if (err != nil) != tc.wantErr {
				t.Fatalf("parseParamArgs(%v) err = %v, wantErr %v", tc.args, err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if got.spec != tc.wantSpec {
				t.Errorf("spec = %q, want %q", got.spec, tc.wantSpec)
			}
			if got.baseURL != tc.wantBase {
				t.Errorf("baseURL = %q, want %q", got.baseURL, tc.wantBase)
			}
		})
	}
}
