.PHONY: build clean install

VERSION := 0.1.0
GOARCH ?= $(shell go env GOARCH)
LDFLAGS := -ldflags "-s -w -X main.version=$(VERSION)"

build:
	CGO_ENABLED=1 GOARCH=$(GOARCH) GOOS=darwin \
	  go build $(LDFLAGS) -o bin/mactop ./cmd/mactop

install: build
	cp bin/mactop /usr/local/bin/mactop

clean:
	rm -rf bin/
