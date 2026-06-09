-include .env
export

# Build configuration
VERSION ?= 1.0.0
VERSION_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
VERSION_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BINARY_NAME := b4
SRC_DIR := ./src
OUT_DIR := ./out

# Build flags
CGO_ENABLED ?= 0
LDFLAGS := -s -w -X main.Version=$(VERSION) -X main.Commit=$(VERSION_COMMIT) -X main.Date=$(VERSION_DATE)
BUILDFLAGS := -trimpath

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

.PHONY: swagger
swagger:
	@if [ "$(VERSION)" = "1.0.0" ]; then echo "ERROR: specify VERSION, e.g. make swagger VERSION=1.48.0"; exit 1; fi
	@echo "Generating Swagger docs..."
	@cd $(SRC_DIR) && go run github.com/swaggo/swag/cmd/swag@v1.16.4 init --generalInfo main.go --output tools/docs --parseDependency
	@sed -i 's/"version": *"[^"]*"/"version": "$(VERSION)"/' $(SRC_DIR)/tools/docs/swagger.json
	@cp $(SRC_DIR)/tools/docs/swagger.json ./docs/static/swagger.json
	@rm -rf $(SRC_DIR)/tools/docs
	@mkdir -p docs/static/swagger-versions
	@cp docs/static/swagger.json "docs/static/swagger-versions/v$(VERSION).json"
	@echo "[" > docs/static/swagger-versions/index.json
	@first=true; for f in $$(ls docs/static/swagger-versions/v*.json | sort -rV); do \
		ver=$$(basename "$$f" .json); \
		[ "$$first" = true ] && first=false || echo "," >> docs/static/swagger-versions/index.json; \
		echo "  \"$$ver\"" >> docs/static/swagger-versions/index.json; \
	done
	@echo "]" >> docs/static/swagger-versions/index.json
	@echo "Swagger spec (v$(VERSION)) copied to docs/static/"

# Build for current platform
.PHONY: build
build: swagger
	@echo "Building $(BINARY_NAME) $(VERSION) for current platform..."
	@mkdir -p $(OUT_DIR)
	go -C $(SRC_DIR) build $(BUILDFLAGS) -ldflags "$(LDFLAGS)" -o ../$(OUT_DIR)/$(BINARY_NAME)

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
		go -C $(SRC_DIR) build $(BUILDFLAGS) -ldflags "$(LDFLAGS)" \
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

.PHONY: build-docker
build-docker:
	@echo "Building Docker image..."
	@DOCKER_BUILDKIT=1 docker build -t b4:test .

.PHONY: run-docker
run-docker:
	@echo "Running Docker container..."
	@docker run --rm -it --cap-add=NET_ADMIN --cap-add=NET_RAW -p 7000:7000 -v ./out/linux-amd64:/etc/b4 b4:test --config /etc/b4/b4.json



.PHONY: gen-defaults
gen-defaults:
	@echo "Generating UI defaults from Go..."
	@cd $(SRC_DIR) && go run tools/gendefaults.go

.PHONY: build-ui
build-ui: gen-defaults
	@echo "Building web UI..."
	@cd src/http/ui && pnpm build
	@echo "Web UI build complete."

SFTP_PORT ?= 22

.PHONY: deploy-%
deploy-%:

	@$(eval ARCH := $(subst deploy-,,$@))

	@if [ ! -f .env ]; then \
		echo "Error: .env file not found. Create one from .env.example"; \
		exit 1; \
	fi
	@if [ -z "$(SFTP_HOST)" ] || [ -z "$(SFTP_PATH)" ] || [ -z "$(SFTP_USER)" ]; then \
		echo "Error: SFTP_HOST, SFTP_USER and SFTP_PATH must be set in .env"; \
		exit 1; \
	fi

	@$(MAKE) --no-print-directory build-ui
	@$(MAKE) --no-print-directory $(ARCH)

	@$(MAKE) --no-print-directory linux-$(ARCH)
	@echo "Uploading to $(SFTP_USER)@$(SFTP_HOST):$(SFTP_PATH)/$(BINARY_NAME)..."
	@if [ -n "$(SFTP_PASS)" ]; then \
		echo "put $(OUT_DIR)/linux-$(ARCH)/$(BINARY_NAME) $(SFTP_PATH)/$(BINARY_NAME)" | \
			sshpass -p '$(SFTP_PASS)' sftp -oStrictHostKeyChecking=no -P $(SFTP_PORT) $(SFTP_USER)@$(SFTP_HOST); \
	else \
		echo "put $(OUT_DIR)/linux-$(ARCH)/$(BINARY_NAME) $(SFTP_PATH)/$(BINARY_NAME)" | \
			sftp -P $(SFTP_PORT) $(SFTP_USER)@$(SFTP_HOST); \
	fi
	@echo "Deploy complete!"

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
	@printf "  %-25s %s\n" "make deploy-<arch>" "Build and upload via SFTP (requires .env)"
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