#!/bin/bash
set -euo pipefail

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

# Map architecture to Go's naming convention
case "${ARCH}" in
  x86_64)
    GOARCH="amd64"
    ;;
  aarch64)
    GOARCH="arm64"
    ;;
  arm64)
    GOARCH="arm64"
    ;;
  *)
    echo "Unsupported architecture: ${ARCH}"
    exit 1
    ;;
esac

PLATFORM="${OS}-${GOARCH}"

if [ ! -d ".go/bin" ]; then
  echo "Downloading Go ${GO_VERSION} for ${PLATFORM}..."
  curl --silent --show-error --location --remote-name "https://go.dev/dl/go${GO_VERSION}.${PLATFORM}.tar.gz"
  mkdir -p .go
  tar -C .go --strip-components=1 -xzf "go${GO_VERSION}.${PLATFORM}.tar.gz"
  rm "go${GO_VERSION}.${PLATFORM}.tar.gz"
  echo "Go ${GO_VERSION} installed to .go/"
else
  echo "Go already installed in .go/"
fi

export PATH="${PWD}/.go/bin:${PATH}"
.go/bin/go version
