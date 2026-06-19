GO_TEST_FLAGS := -race -coverprofile="coverage.txt" -coverpkg=github.com/pierskarsenbarg/pulumi-openapi-provider/...
GO_TEST       := go test $(GO_TEST_FLAGS)

EXAMPLE_DIRS := $(wildcard examples/*/)
EXAMPLES     := $(notdir $(EXAMPLE_DIRS))

.SECONDARY:

.PHONY: build test tidy build-examples schema gen-sdk clean \
        $(addprefix schema-, $(EXAMPLES)) \
        $(addprefix gen-sdk-, $(EXAMPLES))

build:
	go build ./...

test:
	$(GO_TEST) ./...

tidy:
	@for f in $$(find . -name go.mod); do \
		cd $$(dirname $$f) || exit 1; \
		echo "tidying $$f"; \
		go mod tidy || exit 1; \
		cd - > /dev/null; done

build-examples: $(foreach e,$(EXAMPLES),bin/examples/pulumi-resource-$(e))

bin/examples/pulumi-resource-%:
	go build -C examples/$* -o ../../bin/examples/pulumi-resource-$* .

schema: $(foreach e,$(EXAMPLES),examples/$(e)/schema.json)

examples/%/schema.json: bin/examples/pulumi-resource-%
	pulumi package get-schema ./$< > $@

gen-sdk: $(foreach e,$(EXAMPLES),gen-sdk-$(e))

gen-sdk-%: examples/%/schema.json
	pulumi package gen-sdk ./bin/examples/pulumi-resource-$* --language all --out examples/$*/sdk

clean:
	rm -rf bin/examples coverage.txt
