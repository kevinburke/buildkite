.PHONY: emoji

test:
	go test ./...

release:
	bump_version --tag-prefix=v minor lib/lib.go

emoji:
	mkdir -p emoji
	cp ../../buildkite/cli/local/emoji.go ./emoji/emoji.go

emoji/data:
	mkdir -p emoji/data
	curl --remote-name --output-dir emoji/data --silent --location https://github.com/buildkite/emojis/raw/main/img-apple-64.json
	curl --remote-name --output-dir emoji/data --silent --location https://github.com/buildkite/emojis/raw/main/img-buildkite-64.json

emoji/assets/assets.go: Makefile
	mkdir -p emoji/assets
	go-bindata -o emoji/assets/assets.go --prefix emoji/data --nocompress --nometadata --pkg assets ./emoji/data/...

emoji/assets: emoji/assets/assets.go

assets: emoji/assets
