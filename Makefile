APP_NAME := mahpastes
BUILD_DIR := build/bin

# OS detection
ifeq ($(OS),Windows_NT)
    GO_BIN := $(strip $(GOBIN))
    GO_PATH := $(strip $(GOPATH))
    WAILS := wails
    APP_EXE := $(BUILD_DIR)/$(APP_NAME).exe
    INSTALL_DIR := $(APPDATA)/$(APP_NAME)
    PLUGIN_DIR := $(APPDATA)/$(APP_NAME)/plugins
    MP_INSTALL_DIR ?= $(if $(GO_BIN),$(GO_BIN),$(if $(GO_PATH),$(GO_PATH)\bin,$(USERPROFILE)\go\bin))
    SHELL := cmd.exe
    .SHELLFLAGS := /c
else
    GO_BIN := $(strip $(shell go env GOBIN 2>/dev/null))
    WAILS := ~/go/bin/wails
    APP_BUNDLE := $(BUILD_DIR)/$(APP_NAME).app
    INSTALL_DIR := /Applications
    PLUGIN_DIR := $(HOME)/Library/Application Support/mahpastes/plugins
    MP_INSTALL_DIR ?= $(if $(GO_BIN),$(GO_BIN),$(HOME)/.local/bin)
endif

.PHONY: dev build clean install uninstall bindings mp mp-install mp-cross test screenshots

## Development

dev: ## Start development server with hot reload
	$(WAILS) dev

## Build

build: clean ## Production build (clean)
	$(WAILS) build

ifeq ($(OS),Windows_NT)
clean: ## Remove build artifacts
	if exist "$(subst /,\,$(BUILD_DIR))" rd /s /q "$(subst /,\,$(BUILD_DIR))"
else
clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR)
endif

bindings: ## Regenerate Wails frontend bindings after Go changes
	$(WAILS) generate module

## Install

ifeq ($(OS),Windows_NT)
install: build ## Build and install (kills running instance)
	-taskkill /IM "$(APP_NAME).exe" /F 2>nul
	timeout /t 1 /nobreak >nul
	if not exist "$(INSTALL_DIR)" mkdir "$(INSTALL_DIR)"
	copy "$(subst /,\,$(APP_EXE))" "$(INSTALL_DIR)\$(APP_NAME).exe"
	@echo Installed to $(INSTALL_DIR)\$(APP_NAME).exe
	if not exist "$(PLUGIN_DIR)" mkdir "$(PLUGIN_DIR)"
	copy plugins\*.lua "$(PLUGIN_DIR)\"
	@echo Updated bundled plugins
	start "" "$(INSTALL_DIR)\$(APP_NAME).exe"

uninstall: ## Remove installed app
	-taskkill /IM "$(APP_NAME).exe" /F 2>nul
	if exist "$(INSTALL_DIR)" rd /s /q "$(INSTALL_DIR)"
	@echo Removed $(APP_NAME) from $(INSTALL_DIR)
else
install: build ## Build and install (kills running instance)
	@pkill -f "$(APP_NAME).app/Contents/MacOS/$(APP_NAME)" 2>/dev/null || true
	@sleep 1
	rm -rf $(INSTALL_DIR)/$(APP_NAME).app
	cp -R $(APP_BUNDLE) $(INSTALL_DIR)/$(APP_NAME).app
	xattr -cr $(INSTALL_DIR)/$(APP_NAME).app
	@echo "Installed to $(INSTALL_DIR)/$(APP_NAME).app"
	@# Update bundled plugins
	@mkdir -p "$(PLUGIN_DIR)"
	@cp plugins/*.lua "$(PLUGIN_DIR)/"
	@echo "Updated bundled plugins"
	open $(INSTALL_DIR)/$(APP_NAME).app

uninstall: ## Remove installed app
	@pkill -f "$(APP_NAME).app/Contents/MacOS/$(APP_NAME)" 2>/dev/null || true
	rm -rf $(INSTALL_DIR)/$(APP_NAME).app
	@echo "Removed $(APP_NAME) from $(INSTALL_DIR)"
endif

## CLI

mp: ## Build mp CLI for current platform
	go build -o build/bin/mp ./cmd/mp

mp-install: mp ## Install mp to MP_INSTALL_DIR (user-writable by default)
ifeq ($(OS),Windows_NT)
	if not exist "$(MP_INSTALL_DIR)" mkdir "$(MP_INSTALL_DIR)"
	copy build\bin\mp.exe "$(MP_INSTALL_DIR)\mp.exe"
	@echo Installed mp to $(MP_INSTALL_DIR)\mp.exe
	@echo Add $(MP_INSTALL_DIR) to PATH to run mp from a new terminal.
else
	@mkdir -p "$(MP_INSTALL_DIR)"
	@install -m 755 build/bin/mp "$(MP_INSTALL_DIR)/mp"
	@echo "Installed mp to $(MP_INSTALL_DIR)/mp"
	@case ":$$PATH:" in *:"$(MP_INSTALL_DIR)":*) ;; *) echo "Add $(MP_INSTALL_DIR) to PATH to run 'mp' directly." ;; esac
endif

mp-cross: ## Cross-compile mp for all platforms
	GOOS=darwin GOARCH=amd64 go build -o build/bin/mp-darwin-amd64 ./cmd/mp
	GOOS=darwin GOARCH=arm64 go build -o build/bin/mp-darwin-arm64 ./cmd/mp
	GOOS=linux GOARCH=amd64 go build -o build/bin/mp-linux-amd64 ./cmd/mp
	GOOS=windows GOARCH=amd64 go build -o build/bin/mp-windows-amd64.exe ./cmd/mp

## Testing

test: ## Run e2e tests
	cd e2e && npm test

test-headed: ## Run e2e tests with visible browser
	cd e2e && npm run test:headed

test-debug: ## Run e2e tests with Playwright inspector
	cd e2e && npm run test:debug

screenshots: ## Capture documentation screenshots via Playwright
	cd e2e && npm run screenshots

## Help

# NOTE: The Windows help target lists targets manually because grep/awk are
# unavailable in cmd.exe. Keep this list in sync when adding new targets.
ifeq ($(OS),Windows_NT)
help: ## Show this help
	@echo Available targets:
	@echo   dev            Start development server with hot reload
	@echo   build          Production build (clean)
	@echo   clean          Remove build artifacts
	@echo   install        Build and install (kills running instance)
	@echo   uninstall      Remove installed app
	@echo   bindings       Regenerate Wails frontend bindings after Go changes
	@echo   test           Run e2e tests
	@echo   test-headed    Run e2e tests with visible browser
	@echo   test-debug     Run e2e tests with Playwright inspector
	@echo   screenshots    Capture documentation screenshots via Playwright
	@echo   mp             Build mp CLI for current platform
	@echo   mp-install     Install mp to MP_INSTALL_DIR
	@echo   mp-cross       Cross-compile mp for all platforms
else
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
endif

.DEFAULT_GOAL := help
