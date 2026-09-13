PREFIX ?= $(HOME)/.local
VERSION ?= dev
LDFLAGS = -X main.version=$(VERSION)

.PHONY: build test check install clean cross release

build:
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/systemdoc ./cmd/systemdoc

test:
	go test -race ./...

check:
	sh -n install.sh scripts/release.sh
	sh scripts/test-installer.sh
	go vet ./...
	go test ./...

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
