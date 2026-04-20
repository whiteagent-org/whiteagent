SHELL := /bin/sh

VERSION ?= dev
APP := whiteagent
CMD := ./cmd/whiteagent
CONFIG ?= bin/whiteagent.json
BIN_DIR := bin
APP_BIN := $(BIN_DIR)/$(APP)
PLUGIN_DIST := $(BIN_DIR)/plugins
SO_BUILDER = go build -buildmode=plugin -o

.PHONY: help fmt test build run validate-config plugin-list tidy plugins clean

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "%-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

fmt: ## Format all Go files
	@gofmt -w $$(find . -name '*.go' | sort)

test: ## Run test suite
	@go test ./...

build: plugins ## Build whiteagent binary into bin/
	@mkdir -p $(BIN_DIR)
	@go build -ldflags "-X main.version=$(VERSION)" -o $(APP_BIN) $(CMD)

run: ## Run the app with config (Ctrl+C to stop)
	@go run $(CMD) serve --config $(CONFIG)

validate-config: ## Validate config file syntax
	@if [ ! -f "$(CONFIG)" ]; then \
		echo "config file not found: $(CONFIG)"; \
		exit 1; \
	fi
	@python3 -c "import json; json.load(open('$(CONFIG)'))" 2>/dev/null && echo "$(CONFIG): valid JSON" || echo "$(CONFIG): invalid JSON"

tidy: ## Sync go.mod/go.sum
	@go mod tidy

plugins: ## Build all plugin .so files into bin/
	@./scripts/build-plugins.sh

clean: ## Remove local build output
	@rm -rf $(APP_BIN)
	@rm -rf $(PLUGIN_DIST)

# ---------------------------------------------------------------------------
# DinD TLS certificates
# ---------------------------------------------------------------------------

CERT_DIR := docker/dind-certs
CERT_DAYS := 3650

.PHONY: dind-certs dind-certs-clean

dind-certs: $(CERT_DIR)/ca.pem $(CERT_DIR)/server-cert.pem $(CERT_DIR)/cert.pem ## Generate TLS certs for DinD

$(CERT_DIR)/ca.pem:
	@mkdir -p $(CERT_DIR)
	@openssl genrsa -out $(CERT_DIR)/ca-key.pem 4096 2>/dev/null
	@openssl req -new -x509 -days $(CERT_DAYS) -key $(CERT_DIR)/ca-key.pem \
		-out $(CERT_DIR)/ca.pem -subj "/CN=Docker DinD CA" 2>/dev/null
	@echo "CA certificate created"

$(CERT_DIR)/server-cert.pem: $(CERT_DIR)/ca.pem
	@openssl genrsa -out $(CERT_DIR)/server-key.pem 4096 2>/dev/null
	@openssl req -new -key $(CERT_DIR)/server-key.pem \
		-out $(CERT_DIR)/server.csr -subj "/CN=dind" 2>/dev/null
	@printf "subjectAltName=DNS:dind,DNS:localhost,IP:127.0.0.1" > $(CERT_DIR)/server-extfile.cnf
	@openssl x509 -req -days $(CERT_DAYS) -in $(CERT_DIR)/server.csr \
		-CA $(CERT_DIR)/ca.pem -CAkey $(CERT_DIR)/ca-key.pem -CAcreateserial \
		-out $(CERT_DIR)/server-cert.pem -extfile $(CERT_DIR)/server-extfile.cnf 2>/dev/null
	@rm -f $(CERT_DIR)/server.csr $(CERT_DIR)/server-extfile.cnf $(CERT_DIR)/ca.srl
	@echo "Server certificate created"

$(CERT_DIR)/cert.pem: $(CERT_DIR)/ca.pem
	@openssl genrsa -out $(CERT_DIR)/key.pem 4096 2>/dev/null
	@openssl req -new -key $(CERT_DIR)/key.pem \
		-out $(CERT_DIR)/client.csr -subj "/CN=whiteagent" 2>/dev/null
	@printf "extendedKeyUsage=clientAuth" > $(CERT_DIR)/client-extfile.cnf
	@openssl x509 -req -days $(CERT_DAYS) -in $(CERT_DIR)/client.csr \
		-CA $(CERT_DIR)/ca.pem -CAkey $(CERT_DIR)/ca-key.pem -CAcreateserial \
		-out $(CERT_DIR)/cert.pem -extfile $(CERT_DIR)/client-extfile.cnf 2>/dev/null
	@rm -f $(CERT_DIR)/client.csr $(CERT_DIR)/client-extfile.cnf $(CERT_DIR)/ca.srl
	@echo "Client certificate created"

dind-certs-clean: ## Remove DinD TLS certificates
	@rm -rf $(CERT_DIR)
