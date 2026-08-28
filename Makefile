# grease — developer task runner.
#
# The `check` target reproduces locally exactly what CI gates on, so "passes on
# my machine" and "passes in CI" mean the same thing. The release-oriented
# targets (cross, sbom) mirror what the release workflow does, so a release can
# be rehearsed locally before tagging.

BINARY      := grease
PKG         := github.com/muratcanozdemir/grease
BUILDINFO   := $(PKG)/internal/buildinfo
VERSION     := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w \
	-X $(BUILDINFO).Version=$(VERSION) \
	-X $(BUILDINFO).Commit=$(COMMIT) \
	-X $(BUILDINFO).Date=$(DATE)

# Release targets: the five supported platforms.
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all
all: check build

# --- local quality bar (mirrors CI) ---------------------------------------

.PHONY: check
check: fmt-check vet test audit

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: fmt-check
fmt-check:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "not gofmt-clean:"; echo "$$unformatted"; exit 1; \
	fi

.PHONY: vet
vet:
	go vet ./...

.PHONY: test
test:
	go test -race -count=1 -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out | tail -1

.PHONY: audit
audit:
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# --- build ------------------------------------------------------------------

.PHONY: build
build:
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/grease

# Cross-compile every supported platform into dist/, with per-binary checksums.
.PHONY: cross
cross:
	@mkdir -p dist
	@for platform in $(PLATFORMS); do \
		goos=$${platform%/*}; goarch=$${platform#*/}; \
		ext=""; [ "$$goos" = "windows" ] && ext=".exe"; \
		out="$(BINARY)-$(VERSION)-$$goos-$$goarch$$ext"; \
		echo "building $$out"; \
		CGO_ENABLED=0 GOOS=$$goos GOARCH=$$goarch \
			go build -trimpath -ldflags '$(LDFLAGS)' -o "dist/$$out" ./cmd/grease || exit 1; \
		( cd dist && sha256sum "$$out" >> SHA256SUMS ); \
	done
	@echo "dist/ contents:"; ls -la dist/

# --- SBOM -------------------------------------------------------------------

.PHONY: sbom
sbom:
	go run github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest \
		mod -licenses -json -output $(BINARY)-sbom.cdx.json
	@echo "wrote $(BINARY)-sbom.cdx.json"

# --- maintenance ------------------------------------------------------------

.PHONY: pin-actions
pin-actions:
	./scripts/pin-actions.sh

.PHONY: tidy
tidy:
	go mod tidy

.PHONY: clean
clean:
	rm -rf dist $(BINARY) coverage.out $(BINARY)-sbom.cdx.json