PROVIDER_BINARY := bin/pulumi-resource-testapi
SCHEMA_FILE     := schema.json
SDK_DIR         := pulumi/sdk

.PHONY: all setup install-api migrate build-provider schema sdk install-pulumi clean run-api test

# Full one-shot setup (requires API to already be running for schema/sdk steps)
all: build-provider schema sdk install-pulumi

# Install API dependencies
install-api:
	cd api && bun install

# Run the API server (blocks; run in a separate terminal before other targets)
run-api: install-api migrate
	cd api && bun run start

# Run DB migrations
migrate: install-api
	cd api && bun run migrate

# Build the provider binary
build-provider:
	go build -C provider -o ../$(PROVIDER_BINARY) .

# Generate schema from a running API (requires run-api in another terminal)
schema: build-provider
	pulumi package get-schema ./$(PROVIDER_BINARY) > $(SCHEMA_FILE)

# Generate TypeScript SDK from schema
sdk: schema
	pulumi package gen-sdk ./$(PROVIDER_BINARY) --language nodejs --out $(SDK_DIR)

# Install Pulumi program dependencies (after SDK is generated)
install-pulumi: sdk
	cd pulumi && npm install

# Run integration tests: pulumi up then pulumi destroy
test: install-pulumi
	cd pulumi && pulumi stack init test --non-interactive || true
	cd pulumi && pulumi config set testapi:baseUrl http://localhost:3000 --non-interactive
	cd pulumi && pulumi up --yes --non-interactive
	cd pulumi && pulumi destroy --yes --non-interactive
	cd pulumi && pulumi stack rm test --yes --non-interactive

clean:
	rm -rf bin $(SCHEMA_FILE) $(SDK_DIR) api/dev.db api/migrations pulumi/bin pulumi/node_modules
