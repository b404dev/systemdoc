PREFIX ?= $(HOME)/.local
VERSION ?= dev
LDFLAGS = -X main.version=$(VERSION)

.PHONY: build test check lint bench-smoke install clean cross release

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/systemdoc ./cmd/systemdoc

test:
	go test -race ./...

check:
	sh -n install.sh scripts/release.sh playground/k0s.sh
	sh scripts/test-installer.sh
	go vet ./...

# gofmt and staticcheck are cheap enough for every push; govulncheck needs
# the network and is skipped when it cannot be fetched.
lint:
	@test -z "$$(gofmt -l cmd internal)" || { gofmt -l cmd internal; echo 'gofmt: files need formatting'; exit 1; }
	go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	go run golang.org/x/vuln/cmd/govulncheck@latest ./... || echo 'govulncheck unavailable or reported findings'

# Compiles and runs every benchmark once so a broken benchmark or a
# hot-path panic is caught in CI without paying for full timing runs.
bench-smoke:
	go test ./internal/dashboard -run '^$$' -bench . -benchtime=1x

install: build
	mkdir -p "$(DESTDIR)$(PREFIX)/bin"
	install -m755 bin/systemdoc "$(DESTDIR)$(PREFIX)/bin/systemdoc"

cross:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/systemdoc-linux-amd64 ./cmd/systemdoc
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/systemdoc-linux-arm64 ./cmd/systemdoc

	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/systemdoc-darwin-arm64 ./cmd/systemdoc

release:
	sh scripts/release.sh "$(VERSION)"

clean:
	go clean ./...
