ifneq (,$(wildcard ./.env))
    include .env
    export
endif

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

BUILD_CMD = go build -ldflags "-w -linkmode external -extldflags '-lstdc++' -X main.version=$(VERSION)" -buildmode pie
BUILD_SRC = ./cmd/beetroot-dl
BUILD_DIR = ./bin

ZIG_CC = zig cc
ZIG_CXX = zig c++

MACOS_SDK_PATH ?= /Library/Developer/CommandLineTools/SDKs/MacOSX.sdk

# Native host toolchain (build/install/test): taglib headers and libs are
# found via pkg-config, falling back to Homebrew's prefix. The zig targets
# below are only for release cross-builds.
TAGLIB_CFLAGS ?= $(shell pkg-config --cflags taglib_c 2>/dev/null || echo -I$(shell brew --prefix taglib 2>/dev/null || echo /usr/local)/include)
TAGLIB_LDFLAGS ?= $(shell pkg-config --libs-only-L taglib_c 2>/dev/null || echo -L$(shell brew --prefix taglib 2>/dev/null || echo /usr/local)/lib)
NATIVE_ENV = CGO_ENABLED=1 CGO_CFLAGS="$(TAGLIB_CFLAGS)" CGO_CXXFLAGS="$(TAGLIB_CFLAGS) -std=c++17" CGO_LDFLAGS="$(TAGLIB_LDFLAGS)"

GO_TEST_FLAGS ?=

.PHONY: all build install test vet

all: darwin-arm64 darwin-amd64 linux-amd64 linux-arm64 windows-amd64

build:
	$(NATIVE_ENV) go build -ldflags "-X main.version=$(VERSION)" -o $(BUILD_DIR)/beetroot-dl $(BUILD_SRC)

install:
	$(NATIVE_ENV) go install -ldflags "-X main.version=$(VERSION)" $(BUILD_SRC)

test:
	$(NATIVE_ENV) go test $(GO_TEST_FLAGS) ./...

vet:
	$(NATIVE_ENV) go vet ./...

darwin-arm64:
	@echo "Building for macOS ARM64"
	go clean -cache
	CGO_ENABLED=1 \
	GOOS=darwin \
	GOARCH=arm64 \
	CGO_LDFLAGS="-F${MACOS_SDK_PATH}/System/Library/Frameworks -L${MACOS_SDK_PATH}/usr/lib" \
	CC="${ZIG_CC} -target aarch64-macos ${MACOS_ARM64_LIB_PATH} -isysroot ${MACOS_SDK_PATH} -iwithsysroot /usr/include -iframeworkwithsysroot /System/Library/Frameworks" \
	CXX="${ZIG_CXX} -target aarch64-macos ${MACOS_ARM64_LIB_PATH} -isysroot ${MACOS_SDK_PATH} -iwithsysroot /usr/include -iframeworkwithsysroot /System/Library/Frameworks" \
	${BUILD_CMD} -o=${BUILD_DIR}/beetroot-dl-darwin-arm64 ${BUILD_SRC}

darwin-amd64:
	@echo "Building for macOS AMD64"
	go clean -cache
	CGO_ENABLED=1 \
	GOOS=darwin \
	GOARCH=amd64 \
	CGO_LDFLAGS="-F${MACOS_SDK_PATH}/System/Library/Frameworks -L${MACOS_SDK_PATH}/usr/lib" \
	CC="${ZIG_CC} -target x86_64-macos ${MACOS_AMD64_LIB_PATH} -isysroot ${MACOS_SDK_PATH} -iwithsysroot /usr/include -iframeworkwithsysroot /System/Library/Frameworks" \
	CXX="${ZIG_CXX} -target x86_64-macos ${MACOS_AMD64_LIB_PATH} -isysroot ${MACOS_SDK_PATH} -iwithsysroot /usr/include -iframeworkwithsysroot /System/Library/Frameworks" \
	${BUILD_CMD} -o=${BUILD_DIR}/beetroot-dl-darwin-amd64 ${BUILD_SRC}

linux-amd64:
	@echo "Building for Linux AMD64"
	go clean -cache
	CGO_ENABLED=1 \
	GOOS=linux \
	GOARCH=amd64 \
	CC="${ZIG_CC} -target x86_64-linux-gnu ${LINUX_AMD64_LIB_PATH} -DTAGLIB_STATIC -Wall" \
	CXX="${ZIG_CXX} -target x86_64-linux-gnu ${LINUX_AMD64_LIB_PATH} -DTAGLIB_STATIC -Wall" \
	${BUILD_CMD} -o=${BUILD_DIR}/beetroot-dl-linux-amd64 ${BUILD_SRC}

linux-arm64:
	@echo "Building for Linux ARM64"
	go clean -cache
	CGO_ENABLED=1 \
	GOOS=linux \
	GOARCH=arm64 \
	CC="${ZIG_CC} -target aarch64-linux-gnu ${LINUX_ARM64_LIB_PATH} -DTAGLIB_STATIC -Wall" \
	CXX="${ZIG_CXX} -target aarch64-linux-gnu ${LINUX_ARM64_LIB_PATH} -DTAGLIB_STATIC -Wall" \
	${BUILD_CMD} -o=${BUILD_DIR}/beetroot-dl-linux-arm64 ${BUILD_SRC}

windows-amd64:
	@echo "Building for Windows AMD64"
	go clean -cache
	CGO_ENABLED=1 \
	GOOS=windows \
	GOARCH=amd64 \
	CC="${ZIG_CC} -target x86_64-windows-gnu ${WINDOWS_AMD64_LIB_PATH} -DTAGLIB_STATIC -Wall -Wno-deprecated" \
	CXX="${ZIG_CXX} -target x86_64-windows-gnu ${WINDOWS_AMD64_LIB_PATH} -DTAGLIB_STATIC -Wall -Wno-deprecated" \
	${BUILD_CMD} -o=${BUILD_DIR}/beetroot-dl-windows-amd64.exe ${BUILD_SRC}