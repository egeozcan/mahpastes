WAILS := ~/go/bin/wails
APP_NAME := mahpastes
BUILD_DIR := build/bin
APP_BUNDLE := $(BUILD_DIR)/$(APP_NAME).app
INSTALL_DIR := /Applications

.PHONY: dev build clean install uninstall bindings test

## Development

dev: ## Start development server with hot reload
	$(WAILS) dev

## Build

build: clean ## Production build (clean)
	$(WAILS) build

clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR)

bindings: ## Regenerate Wails frontend bindings after Go changes
	$(WAILS) generate module

## Install

install: build ## Build and install to /Applications (kills running instance)
	@pkill -f "$(APP_NAME).app/Contents/MacOS/$(APP_NAME)" 2>/dev/null || true
	@sleep 1
	rm -rf $(INSTALL_DIR)/$(APP_NAME).app
	cp -R $(APP_BUNDLE) $(INSTALL_DIR)/$(APP_NAME).app
	xattr -cr $(INSTALL_DIR)/$(APP_NAME).app
	@echo "Installed to $(INSTALL_DIR)/$(APP_NAME).app"
	open $(INSTALL_DIR)/$(APP_NAME).app

uninstall: ## Remove from /Applications
	@pkill -f "$(APP_NAME).app/Contents/MacOS/$(APP_NAME)" 2>/dev/null || true
	rm -rf $(INSTALL_DIR)/$(APP_NAME).app
	@echo "Removed $(APP_NAME) from $(INSTALL_DIR)"

## Testing

test: ## Run e2e tests
	cd e2e && npm test

test-headed: ## Run e2e tests with visible browser
	cd e2e && npm run test:headed

test-debug: ## Run e2e tests with Playwright inspector
	cd e2e && npm run test:debug

## Help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'

.DEFAULT_GOAL := help
