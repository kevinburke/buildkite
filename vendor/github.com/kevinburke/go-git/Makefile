.PHONY: test

BENCHSTAT := $(GOPATH)/bin/benchstat
BUMP_VERSION := $(GOPATH)/bin/bump_version
GODOCDOC := $(GOPATH)/bin/godocdoc
UNAME := $(shell uname -s)

$(GOPATH)/bin:
	mkdir -p $(GOPATH)/bin

lint: | $(MEGACHECK)
	GO111MODULE=on go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	go vet ./...

$(GODOCDOC):
	go get github.com/kevinburke/godocdoc

docs: $(GODOCDOC)
	$(GODOCDOC)

test: lint
	@# this target should always be listed first so "make" runs the tests.
	go test ./...

race-test: lint
	go test -race ./...

release: | $(BUMP_VERSION)
	bump_version minor git.go
