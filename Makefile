test:
	staticcheck ./...
	go vet ./...
	go test -trimpath ./...

release:
	bump_version --tag-prefix=v minor lib/lib.go

force: ;

AUTHORS.txt: force | $(WRITE_MAILMAP)
	go install github.com/kevinburke/write_mailmap
	write_mailmap > AUTHORS.txt

authors: AUTHORS.txt

fmt:
	go fmt ./...
