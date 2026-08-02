VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

BUILD_SRC = ./cmd/beetroot-dl
BUILD_DIR = ./bin
LDFLAGS = -ldflags "-s -w -X main.version=$(VERSION)"
GO_TEST_FLAGS ?=

# Pure Go (taglib runs as embedded WebAssembly), so CGO stays off everywhere
# and cross-compiling is just GOOS/GOARCH.
export CGO_ENABLED = 0

.PHONY: all build install test vet clean \
	darwin-arm64 darwin-amd64 linux-amd64 linux-arm64 windows-amd64

build:
	go build $(LDFLAGS) -o $(BUILD_DIR)/beetroot-dl $(BUILD_SRC)

install:
	go install $(LDFLAGS) $(BUILD_SRC)

test:
	go test $(GO_TEST_FLAGS) ./...

vet:
	go vet ./...

clean:
	rm -rf $(BUILD_DIR)

# Release cross-builds.
all: darwin-arm64 darwin-amd64 linux-amd64 linux-arm64 windows-amd64

darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/beetroot-dl-darwin-arm64 $(BUILD_SRC)

darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/beetroot-dl-darwin-amd64 $(BUILD_SRC)

linux-amd64:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/beetroot-dl-linux-amd64 $(BUILD_SRC)

linux-arm64:
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/beetroot-dl-linux-arm64 $(BUILD_SRC)

windows-amd64:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/beetroot-dl-windows-amd64.exe $(BUILD_SRC)
