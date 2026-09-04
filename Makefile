BIN := bin/metasocial-mcp
PKG := ./cmd/metasocial-mcp

.PHONY: build install run test check vet fmt lint tidy e2e clean

build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -o $(BIN) $(PKG)

run: build
	$(BIN)

test:
	go test ./...

vet:
	go vet ./...

fmt:
	@test -z "$$(gofmt -l . | tee /dev/stderr)" || (echo "gofmt: fichiers non formatés"; exit 1)

lint:
	@command -v staticcheck >/dev/null 2>&1 && staticcheck ./... || echo "staticcheck indisponible ou incompatible, ignoré"

# check is the gate every commit must pass.
check: fmt vet lint test

install:
	CGO_ENABLED=0 go build -trimpath -o /usr/local/bin/metasocial-mcp $(PKG)

# e2e starts the binary against a fake Graph API and drives it with the
# official MCP inspector in CLI mode.
e2e: build
	go test -tags=e2e -count=1 -v ./internal/e2e/...

clean:
	rm -rf bin
