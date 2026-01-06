# Build configuration
VERSION ?= 1.0.0
VERSION_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
VERSION_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BINARY_NAME := b4
SRC_DIR := ./src
OUT_DIR := ./out

# Build flags
CGO_ENABLED ?= 0
LDFLAGS := -X main.Version=$(VERSION) -X main.Commit=$(VERSION_COMMIT) -X main.Date=$(VERSION_DATE)

# Linux architectures
LINUX_ARCHS := 386 amd64 arm64 armv5 armv6 armv7 \
               loong64 mips mipsle mips64 mips64le \
               mips_softfloat mipsle_softfloat \
               ppc64 ppc64le riscv64 s390x

# Android architectures (optional)
ANDROID_ARCHS := amd64 arm64 armv7
ANDROID_MIN_API := 21

# Default target
.DEFAULT_GOAL := build

# Build for current platform
.PHONY: build
build:
	@echo "Building $(BINARY_NAME) $(VERSION) for current platform..."
	@mkdir -p $(OUT_DIR)
	go -C $(SRC_DIR) build -ldflags "$(LDFLAGS)" -o ../$(OUT_DIR)/$(BINARY_NAME)

# Build for all Linux architectures
.PHONY: build-all
build-all: clean
	@echo "Building $(BINARY_NAME) $(VERSION) for all Linux architectures..."
	@mkdir -p $(OUT_DIR)/assets
	@$(MAKE) --no-print-directory linux-all
	@echo "Build complete! Assets in $(OUT_DIR)/assets/"

# Build for specific architecture
# Usage: make linux-amd64
.PHONY: linux-%
linux-%:
	@$(eval ARCH := $(subst linux-,,$@))
	@$(eval TARGET := $(ARCH))
	@case $(ARCH) in \
		armv5) GOARCH=arm GOARM=5 ;; \
		armv6) GOARCH=arm GOARM=6 ;; \
		armv7) GOARCH=arm GOARM=7 ;; \
		mips_softfloat)   GOARCH=mips   GOMIPS=softfloat ;; \
		mipsle_softfloat) GOARCH=mipsle GOMIPS=softfloat ;; \
		*)     GOARCH=$(ARCH) ;; \
	esac; \
	$(MAKE) --no-print-directory build-target GOOS=linux GOARCH=$$GOARCH GOARM=$$GOARM GOMIPS=$$GOMIPS TARGET=$(TARGET)

# Build all Linux targets
.PHONY: linux-all
linux-all:
	@for arch in $(LINUX_ARCHS); do \
		$(MAKE) --no-print-directory linux-$$arch; \
	done

# Android builds (optional - requires ANDROID_NDK_HOME)
.PHONY: android
android:
	@if [ -z "$$ANDROID_NDK_HOME" ]; then \
		echo "Error: ANDROID_NDK_HOME not set. Skipping Android builds."; \
		exit 1; \
	fi
	@echo "Building for Android..."
	@for arch in $(ANDROID_ARCHS); do \
		$(MAKE) --no-print-directory android-$$arch; \
	done

# Build specific Android architecture
.PHONY: android-%
android-%:
	@$(eval ARCH := $(subst android-,,$@))
	@case $(ARCH) in \
		amd64) CC="$$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-*/bin/x86_64-linux-android$(ANDROID_MIN_API)-clang" GOARCH=amd64 ;; \
		arm64) CC="$$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-*/bin/aarch64-linux-android$(ANDROID_MIN_API)-clang" GOARCH=arm64 ;; \
		armv7) CC="$$ANDROID_NDK_HOME/toolchains/llvm/prebuilt/linux-*/bin/armv7a-linux-androideabi$(ANDROID_MIN_API)-clang" GOARCH=arm GOARM=7 ;; \
		*) echo "Unsupported Android arch: $(ARCH)"; exit 1 ;; \
	esac; \
	$(MAKE) --no-print-directory build-target GOOS=android GOARCH=$$GOARCH GOARM=$$GOARM TARGET=$(ARCH) CGO_ENABLED=1 CC=$$CC

# Generic build target (internal use)
.PHONY: build-target
build-target:
	@OUT_PATH="$(OUT_DIR)/$(GOOS)-$(TARGET)"
	@echo "  → $(GOOS)/$(TARGET)"
	@mkdir -p "$(OUT_DIR)/$(GOOS)-$(TARGET)" "$(OUT_DIR)/assets"
	@GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) GOMIPS=$(GOMIPS) CGO_ENABLED=$(CGO_ENABLED) CC=$(CC) \
		go -C $(SRC_DIR) build -ldflags "$(LDFLAGS)" \
		-o ../$(OUT_DIR)/$(GOOS)-$(TARGET)/$(BINARY_NAME)
	@tar -czf "$(OUT_DIR)/assets/$(BINARY_NAME)-$(GOOS)-$(TARGET).tar.gz" \
		-C "$(OUT_DIR)/$(GOOS)-$(TARGET)" "$(BINARY_NAME)"
	@sha256sum "$(OUT_DIR)/assets/$(BINARY_NAME)-$(GOOS)-$(TARGET).tar.gz" \
		> "$(OUT_DIR)/assets/$(BINARY_NAME)-$(GOOS)-$(TARGET).tar.gz.sha256"

# Quick builds for common platforms
.PHONY: amd64
amd64: linux-amd64

.PHONY: arm64
arm64: linux-arm64

.PHONY: arm
arm: linux-armv7

# Development helpers
.PHONY: run
run: build
	sudo $(OUT_DIR)/$(BINARY_NAME)

.PHONY: install
install: build
	sudo cp $(OUT_DIR)/$(BINARY_NAME) /usr/local/bin/

.PHONY: uninstall
uninstall:
	sudo rm -f /usr/local/bin/$(BINARY_NAME)

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning build artifacts..."
	@rm -rf $(OUT_DIR)

# Show version info
.PHONY: version
version:
	@echo "Version: $(VERSION)"
	@echo "Commit:  $(VERSION_COMMIT)"
	@echo "Date:    $(VERSION_DATE)"

.PHONY: build-installer
build-installer:
	@echo "Building installer script..."
	@./installer/_build.sh

.PHONY: watch-installer
watch-installer:
	@./installer/_watch.sh

.PHONY: build-ui
build-ui:
	@echo "Building web UI..."
	@cd src/http/ui && pnpm build
	@echo "Web UI build complete."

# Show help
.PHONY: help
help:
	@echo "B4 Makefile - Linux packet processor build system"
	@echo ""
	@echo "Common targets:"
	@printf "  %-25s %s\n" "make" "Build for current platform"
	@printf "  %-25s %s\n" "make build-all" "Build for all Linux architectures"
	@printf "  %-25s %s\n" "make amd64" "Build for Linux amd64"
	@printf "  %-25s %s\n" "make arm64" "Build for Linux arm64"
	@printf "  %-25s %s\n" "make arm" "Build for Linux armv7"
	@printf "  %-25s %s\n" "make android" "Build for all Android architectures (requires ANDROID_NDK_HOME)"
	@printf "  %-25s %s\n" "make run" "Build and run with sudo"
	@printf "  %-25s %s\n" "make install" "Install to /usr/local/bin"
	@printf "  %-25s %s\n" "make clean" "Remove build artifacts"
	@printf "  %-25s %s\n" "make build-installer" "Build the installer script"
	@printf "  %-25s %s\n" "make watch-installer" "Watch and rebuild installer on changes"
	@printf "  %-25s %s\n" "make build-ui" "Build the web UI"
	@printf "  %-25s %s\n" "make help" "Show this help"
	@echo ""
	@echo "Architecture-specific builds:"
	@printf "  %-25s %s\n" "make linux-<arch>" "Build for specific Linux architecture"
	@printf "  %-25s %s\n" "make android-<arch>" "Build for specific Android architecture"
	@echo ""
	@echo "Available Linux architectures:"
	@echo "  $(LINUX_ARCHS)"
	@echo ""
	@echo "Available Android architectures:"
	@echo "  $(ANDROID_ARCHS)"

# Prevent make from treating command-line targets as files
.PHONY: %