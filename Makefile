BINARY  := whoa
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build test check fmt vet clean

all: check build

build:
	CGO_ENABLED=0 go build -ldflags '$(LDFLAGS)' -o bin/$(BINARY) ./cmd/whoa

test:
	go test -race ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

check: vet test
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "Not gofmt'd:"; echo "$$unformatted"; exit 1; fi

clean:
	rm -rf bin coverage.out coverage.html
