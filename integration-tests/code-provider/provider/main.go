package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/pulumi/pulumi-go-provider/infer"

	openapi "github.com/pierskarsenbarg/pulumi-openapi-provider"
)

const apiBase = "http://localhost:3000"

type OfficeArgs struct {
	Name     string  `pulumi:"name"`
	Location *string `pulumi:"location,optional"`
}

type OfficeState struct {
	OfficeArgs
	CreatedAt *string `pulumi:"createdAt,optional"`
}

type Office struct{}

func (r *Office) Create(ctx context.Context, req infer.CreateRequest[OfficeArgs]) (infer.CreateResponse[OfficeState], error) {
	inputs := req.Inputs
	if req.DryRun {
		return infer.CreateResponse[OfficeState]{Output: OfficeState{OfficeArgs: inputs}}, nil
	}
	body, _ := json.Marshal(map[string]any{"name": inputs.Name, "location": inputs.Location})
	resp, err := http.Post(apiBase+"/offices", "application/json", bytes.NewReader(body))
	if err != nil {
		return infer.CreateResponse[OfficeState]{}, fmt.Errorf("creating office: %w", err)
	}
	defer resp.Body.Close()
	var out officeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return infer.CreateResponse[OfficeState]{}, fmt.Errorf("decoding office response: %w", err)
	}
	return infer.CreateResponse[OfficeState]{
		ID:     out.Id,
		Output: OfficeState{OfficeArgs: inputs, CreatedAt: out.CreatedAt},
	}, nil
}

func (r *Office) Read(ctx context.Context, req infer.ReadRequest[OfficeArgs, OfficeState]) (infer.ReadResponse[OfficeArgs, OfficeState], error) {
	resp, err := http.Get(fmt.Sprintf("%s/offices/%s", apiBase, req.ID))
	if err != nil {
		return infer.ReadResponse[OfficeArgs, OfficeState]{}, fmt.Errorf("reading office: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return infer.ReadResponse[OfficeArgs, OfficeState]{}, nil
	}
	var out officeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return infer.ReadResponse[OfficeArgs, OfficeState]{}, fmt.Errorf("decoding office response: %w", err)
	}
	newInputs := OfficeArgs{Name: out.Name, Location: out.Location}
	return infer.ReadResponse[OfficeArgs, OfficeState]{
		ID:     req.ID,
		Inputs: newInputs,
		State:  OfficeState{OfficeArgs: newInputs, CreatedAt: out.CreatedAt},
	}, nil
}

func (r *Office) Delete(ctx context.Context, req infer.DeleteRequest[OfficeState]) (infer.DeleteResponse, error) {
	httpReq, _ := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/offices/%s", apiBase, req.ID), nil)
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return infer.DeleteResponse{}, fmt.Errorf("deleting office: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return infer.DeleteResponse{}, nil
}

type officeResponse struct {
	Id        string  `json:"id"`
	Name      string  `json:"name"`
	Location  *string `json:"location"`
	CreatedAt *string `json:"createdAt"`
}

func main() {
	ctx := context.Background()
	builder, err := openapi.NewProviderBuilder("testapi", "0.1.0", openapi.Options{
		SpecURL: "http://localhost:3000/openapi",
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := builder.
		WithResources(infer.Resource[*Office](&Office{})).
		Run(ctx); err != nil {
		log.Fatal(err)
	}
}
