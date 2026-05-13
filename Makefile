packages := . ./lib

test:
	staticcheck $(packages)
	go vet $(packages)
	go test -trimpath $(packages)

version ?= minor

.PHONY: release
release: test
	go run github.com/kevinburke/bump_version@latest --tag-prefix=v $(version) lib/version.go

force: ;

AUTHORS.txt: force | $(WRITE_MAILMAP)
	go install github.com/kevinburke/write_mailmap
	write_mailmap > AUTHORS.txt

authors: AUTHORS.txt

fmt:
	go fmt ./...
