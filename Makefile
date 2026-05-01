GOCACHE ?= $(CURDIR)/.gocache

.PHONY: test vet build verify

test:
	GOCACHE=$(GOCACHE) go test ./...

vet:
	GOCACHE=$(GOCACHE) go vet ./...

build:
	GOCACHE=$(GOCACHE) go build -o $(CURDIR)/fundsync ./cmd/fundsync

verify: test vet build
