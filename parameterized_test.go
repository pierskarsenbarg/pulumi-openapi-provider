package openapi

import (
	"testing"
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

func TestParseParamArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantURL string
		wantBase string
		wantErr bool
	}{
		{
			name:    "url only",
			args:    []string{"https://api.example.com/openapi.json"},
			wantURL: "https://api.example.com/openapi.json",
		},
		{
			name:     "url and --base-url= form",
			args:     []string{"https://api.example.com/openapi.json", "--base-url=https://api.example.com"},
			wantURL:  "https://api.example.com/openapi.json",
			wantBase: "https://api.example.com",
		},
		{
			name:     "url and --base-url space form",
			args:     []string{"https://api.example.com/openapi.json", "--base-url", "https://api.example.com"},
			wantURL:  "https://api.example.com/openapi.json",
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
			if got.specURL != tc.wantURL {
				t.Errorf("specURL = %q, want %q", got.specURL, tc.wantURL)
			}
			if got.baseURL != tc.wantBase {
				t.Errorf("baseURL = %q, want %q", got.baseURL, tc.wantBase)
			}
		})
	}
}
