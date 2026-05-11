GOCACHE ?= $(CURDIR)/.gocache

.PHONY: test vet build verify

test:
	GOCACHE=$(GOCACHE) go test ./...

vet:
	GOCACHE=$(GOCACHE) go vet ./...

build:
	GOCACHE=$(GOCACHE) go build -o $(CURDIR)/fundpeek ./cmd/fundpeek

verify: test vet build
