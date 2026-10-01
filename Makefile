SHELL := /bin/bash
BUILD_DIR := ./build
APP_BUNDLE := $(BUILD_DIR)/Telifon.app
CORE_BIN := $(BUILD_DIR)/bin/telifon-cored
UI_BIN := $(BUILD_DIR)/bin/TelifonUI
CORE_TAGS ?= 

.PHONY: all core ui app clean check help

all: core ui app

help:
	@echo "Telifon Build System:"
	@echo "  make all      - Build core, ui, and package Telifon.app"
	@echo "  make core     - Build Go telifon-cored daemon"
	@echo "  make ui       - Build Swift UI executable via SPM"
	@echo "  make app      - Package into $(APP_BUNDLE)"
	@echo "  make clean    - Remove build artifacts"
	@echo "  make check    - Check environment and toolchain"

check:
	@echo "==> Checking build tools..."
	@which swift >/dev/null && echo "  [✓] swift: $$(swift --version | head -n 1)" || (echo "  [✗] swift missing" && exit 1)
	@which go >/dev/null && echo "  [✓] go: $$(go version)" || (echo "  [✗] go missing" && exit 1)
	@which make >/dev/null && echo "  [✓] make: $$(make --version | head -n 1)" || (echo "  [✗] make missing" && exit 1)

core:
	@echo "==> Building Go Core Daemon (telifon-cored)..."
	@mkdir -p $(BUILD_DIR)/bin
	@cd core && GODEBUG=madvdontneed=1 go build \
		$(if $(CORE_TAGS),-tags "$(CORE_TAGS)",) \
		-ldflags="-s -w" \
		-o ../$(CORE_BIN) ./cmd/telifon-cored
	@echo "==> Core built successfully: $(CORE_BIN)"

ui:
	@echo "==> Building Swift UI via SPM..."
	@swift build -c release
	@mkdir -p $(BUILD_DIR)/bin
	@cp -f .build/release/TelifonUI $(UI_BIN)
	@echo "==> UI built successfully: $(UI_BIN)"

app: ui
	@echo "==> Packaging into $(APP_BUNDLE)..."
	@./scripts/bundle_app.sh $(UI_BIN) $(APP_BUNDLE)

clean:
	@echo "==> Cleaning build artifacts..."
	@rm -rf .build $(BUILD_DIR)
	@echo "==> Clean complete."
