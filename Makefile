test:
	go test ./...

release:
	bump_version --tag-prefix=v minor lib/lib.go
