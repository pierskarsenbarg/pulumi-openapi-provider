CODE_BINARY     := code-provider/bin/pulumi-resource-testapi
CODE_SCHEMA     := code-provider/schema.json
CODE_SDK_DIR    := code-provider/pulumi/sdk
PARAM_BINARY    := ../bin/pulumi-resource-openapi-provider
PARAM_PULUMI_DIR := parameterized-provider/pulumi
PARAM_SPEC_FILE := parameterized-provider/openapi.json

export PATH := $(CURDIR)/code-provider/bin:$(PATH)

.PHONY: all install-api generate migrate run-api \
        build-code-provider schema sdk install-code-sdks test-code-provider \
        install-param-plugin test-parameterized-provider \
        clean-param-sdk test-parameterized-provider-from-file \
        test clean

# ─── API (shared) ─────────────────────────────────────────────────────────────

install-api:
	cd api && bun install

generate: install-api
	cd api && bun run generate

migrate: generate
	cd api && bun run migrate

# Blocking; run in a separate terminal before test targets
run-api: migrate
	cd api && bun run start

# ─── Code provider ────────────────────────────────────────────────────────────

build-code-provider:
	go build -C code-provider/provider -o ../bin/pulumi-resource-testapi .

schema: build-code-provider
	pulumi package get-schema ./$(CODE_BINARY) > $(CODE_SCHEMA)

sdk: schema
	pulumi package gen-sdk ./$(CODE_BINARY) --language nodejs --out $(CODE_SDK_DIR)

install-code-sdks: sdk
	cd code-provider/pulumi && pulumi install

test-code-provider: install-code-sdks
	cd code-provider/pulumi && pulumi stack init test --non-interactive || true
	cd code-provider/pulumi && pulumi config set testapi:baseUrl http://localhost:3000 --non-interactive
	cd code-provider/pulumi && pulumi up --yes --non-interactive
	cd code-provider/pulumi && pulumi destroy --yes --non-interactive
	cd code-provider/pulumi && pulumi stack rm test --yes --non-interactive

# ─── Parameterized provider ───────────────────────────────────────────────────

$(PARAM_BINARY):
	cd .. && go build -o bin/pulumi-resource-openapi-provider ./cmd/openapi-provider

install-param-plugin: $(PARAM_BINARY)
	pulumi plugin install resource openapi-provider 0.1.0 --file $(PARAM_BINARY)

test-parameterized-provider: install-param-plugin
	cd $(PARAM_PULUMI_DIR) && pulumi package add openapi-provider http://localhost:3000/openapi
	cd $(PARAM_PULUMI_DIR) && pulumi install
	cd $(PARAM_PULUMI_DIR) && pulumi stack init test --non-interactive || true
	cd $(PARAM_PULUMI_DIR) && pulumi up --yes --non-interactive
	cd $(PARAM_PULUMI_DIR) && pulumi destroy --yes --non-interactive
	cd $(PARAM_PULUMI_DIR) && pulumi stack rm test --yes --non-interactive

clean-param-sdk:
	rm -rf \
		$(PARAM_PULUMI_DIR)/sdk \
		$(PARAM_PULUMI_DIR)/node_modules \
		$(PARAM_PULUMI_DIR)/bin

test-parameterized-provider-from-file: install-param-plugin clean-param-sdk
	curl -sf http://localhost:3000/openapi -o $(PARAM_SPEC_FILE)
	cd $(PARAM_PULUMI_DIR) && pulumi package add openapi-provider ../openapi.json
	cd $(PARAM_PULUMI_DIR) && pulumi install
	cd $(PARAM_PULUMI_DIR) && pulumi stack init test --non-interactive || true
	cd $(PARAM_PULUMI_DIR) && pulumi up --yes --non-interactive
	cd $(PARAM_PULUMI_DIR) && pulumi destroy --yes --non-interactive
	cd $(PARAM_PULUMI_DIR) && pulumi stack rm test --yes --non-interactive
	rm -f $(PARAM_SPEC_FILE)

# ─── Combined ─────────────────────────────────────────────────────────────────

test: test-code-provider test-parameterized-provider test-parameterized-provider-from-file

clean:
	rm -rf \
		bin \
		code-provider/bin \
		$(CODE_SCHEMA) \
		$(CODE_SDK_DIR) \
		$(PARAM_PULUMI_DIR)/sdk \
		$(PARAM_PULUMI_DIR)/node_modules \
		$(PARAM_PULUMI_DIR)/bin \
		$(PARAM_SPEC_FILE) \
		api/dev.db \
		code-provider/pulumi/bin \
		code-provider/pulumi/node_modules \
		api/node_modules
