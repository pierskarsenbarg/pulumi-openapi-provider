package runtime

import (
	"bytes"
	"log/slog"
	"math"
	"strings"
	"testing"

	p "github.com/pulumi/pulumi-go-provider"
	pschema "github.com/pulumi/pulumi/pkg/v3/codegen/schema"

	"github.com/pierskarsenbarg/pulumi-openapi-provider/pkg/config"
	"github.com/pierskarsenbarg/pulumi-openapi-provider/pkg/spec"
)

func TestGetSchema_ReturnsBuildSchemaError(t *testing.T) {
	result := spec.DiscoveryResult{
		Resources: []spec.ResourceDef{{
			Name:  "Widgets",
			Token: "test:index:Widgets",
			InputSchema: map[string]pschema.PropertySpec{
				"size": {TypeSpec: pschema.TypeSpec{Type: "number"}, Default: math.NaN()},
			},
		}},
	}
	cfg := config.New(nil, "", nil, "", nil, "")
	provider := Build("test", "0.0.0", result, cfg, false, PollingConfig{}, nil)

	if _, err := provider.GetSchema(t.Context(), p.GetSchemaRequest{}); err == nil {
		t.Fatal("expected an error from GetSchema, got nil")
	}
}

func TestGetSchema_LogsWarningsOnce(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	result := spec.DiscoveryResult{Warnings: []string{"skipping \"Widgets\": example warning"}}
	cfg := config.New(nil, "", nil, "", nil, "")
	provider := Build("test", "0.0.0", result, cfg, false, PollingConfig{}, nil)

	for range 2 {
		if _, err := provider.GetSchema(t.Context(), p.GetSchemaRequest{}); err != nil {
			t.Fatalf("GetSchema: %v", err)
		}
	}
	if got := strings.Count(buf.String(), "example warning"); got != 1 {
		t.Errorf("warning logged %d times, want 1; output:\n%s", got, buf.String())
	}
}
