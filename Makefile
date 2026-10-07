VERSION ?= 0.0.0-dev
BIN := bin/localdns

.PHONY: build test unit integration cli lint fmt release clean

build:
	go build -trimpath -ldflags "-X github.com/devehab/locly-dns/internal/version.Version=$(VERSION)" -o $(BIN) ./cmd/localdns

test: unit integration cli

unit:
	go test -race ./cmd/... ./internal/...

integration:
	go test -race ./test/integration/...

cli:
	go test ./test/cli/...

lint:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "run: make fmt" && exit 1)
	go vet ./...
	golangci-lint run ./...
	shellcheck install.sh scripts/*.sh

fmt:
	gofmt -w .

release:
	go run ./tools/release -version $(VERSION) -out dist
	cp install.sh install.ps1 dist/

clean:
	rm -rf bin dist
