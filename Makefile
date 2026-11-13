APP_NAME := mahpastes
BUILD_DIR := build/bin

# OS detection
ifeq ($(OS),Windows_NT)
    HOST_OS := Windows
    GO_BIN := $(strip $(GOBIN))
    GO_PATH := $(strip $(GOPATH))
    WAILS := wails
    APP_EXE := $(BUILD_DIR)/$(APP_NAME).exe
    # The program directory must stay apart from the data directory
    # (%APPDATA%\mahpastes holds clips.db), so uninstall can never touch data.
    INSTALL_DIR := $(LOCALAPPDATA)\Programs\$(APP_NAME)
    DATA_DIR := $(APPDATA)\$(APP_NAME)
    START_MENU_LINK := $(APPDATA)\Microsoft\Windows\Start Menu\Programs\$(APP_NAME).lnk
    PLUGIN_DIR := $(APPDATA)/$(APP_NAME)/plugins
    MP_INSTALL_DIR ?= $(if $(GO_BIN),$(GO_BIN),$(if $(GO_PATH),$(GO_PATH)\bin,$(USERPROFILE)\go\bin))
    SHELL := cmd.exe
    .SHELLFLAGS := /c
    # go build -o adds no .exe by itself, and cmd.exe reads a leading
    # "build/bin/mp" as the command "build" with switches.
    MP_BIN := build\bin\mp.exe
    MAHPASTESD_BIN := build\bin\mahpastesd.exe
    # cmd.exe has no VAR=value prefix; each recipe line is its own cmd.exe,
    # so the variables do not outlive the line.
    crossenv = $(foreach v,$(1),set "$(v)"&&)
else
    MP_BIN := build/bin/mp
    MAHPASTESD_BIN := build/bin/mahpastesd
    crossenv = $(1)
    HOST_OS := $(shell uname -s)
    GO_BIN := $(strip $(shell go env GOBIN 2>/dev/null))
    WAILS := ~/go/bin/wails
    APP_BUNDLE := $(BUILD_DIR)/$(APP_NAME).app
    INSTALL_DIR := /Applications
    PLUGIN_DIR := $(HOME)/Library/Application Support/mahpastes/plugins
    # Anchored to the installed executable: pkill -f matches whole command lines,
    # and dev builds, e2e workers and other checkouts run the same bundle name.
    APP_PROCESS_PATTERN := ^$(INSTALL_DIR)/$(APP_NAME)\.app/Contents/MacOS/$(APP_NAME)( |$$)
    FILE_PROVIDER_PROCESS_PATTERN := [/]MahpastesFileProvider\.appex/Contents/MacOS/MahpastesFileProvider
    MP_INSTALL_DIR ?= $(if $(GO_BIN),$(GO_BIN),$(HOME)/.local/bin)
endif

# Version stamped into every binary (see internal/buildinfo and
# scripts/version.sh): "1.5.0" on a release tag, "1.5.0-12-gabc1234" between
# releases, "dev" outside a git checkout. Override with
# `make build VERSION=1.6.0`; a leading "v" is dropped.
ifeq ($(OS),Windows_NT)
    VERSION ?= $(strip $(shell git describe --tags --match "v[0-9]*" 2>NUL))
    COMMIT ?= $(strip $(shell git rev-parse --short=12 HEAD 2>NUL))
else
    VERSION ?= $(shell bash scripts/version.sh --full 2>/dev/null)
    COMMIT ?= $(shell bash scripts/version.sh --commit 2>/dev/null)
endif
override VERSION := $(patsubst v%,%,$(VERSION))
ifeq ($(VERSION),)
    override VERSION := dev
endif
LDFLAGS := -X go-clipboard/internal/buildinfo.Version=$(VERSION) -X go-clipboard/internal/buildinfo.Commit=$(COMMIT)
export MAHPASTES_LDFLAGS := $(LDFLAGS)

.PHONY: dev build clean install install-bundle install-ready-bundle stop-installed-app stop-file-provider-extension uninstall bindings mp mp-install mp-uninstall mp-docs mp-cross mahpastesd mahpastesd-cross mahpastesd-install test screenshots

## Development

dev: ## Start development server with hot reload
ifeq ($(HOST_OS),Linux)
	bash scripts/linux/wails.sh dev
else
	$(WAILS) dev
endif

## Build

# Optional machine-local settings; environment and command-line values can
# override defaults declared with ?= in this file.
ifeq ($(HOST_OS),Darwin)
-include $(HOME)/.config/mahpastes/signing.mk
endif

# Pass signing settings supplied through local config, environment or make arguments.
export TEAM_ID SIGN_IDENTITY HOST_PROFILE_PATH EXTENSION_PROFILE_PATH

build: clean ## Production build (clean; Finder access on macOS when signing is configured)
ifeq ($(HOST_OS),Darwin)
	@if [ -n "$$TEAM_ID" ] && [ -n "$$SIGN_IDENTITY" ] && \
	    [ -f "$$HOST_PROFILE_PATH" ] && [ -r "$$HOST_PROFILE_PATH" ] && \
	    [ -f "$$EXTENSION_PROFILE_PATH" ] && [ -r "$$EXTENSION_PROFILE_PATH" ]; then \
		$(MAKE) build-file-provider; \
	else \
		echo "Warning: Finder integration disabled: set TEAM_ID, SIGN_IDENTITY, HOST_PROFILE_PATH and EXTENSION_PROFILE_PATH (both profiles must be readable files). Building the ordinary app." >&2; \
		$(WAILS) build -ldflags "$(LDFLAGS)"; \
	fi
else ifeq ($(HOST_OS),Linux)
	bash scripts/linux/wails.sh build -ldflags "$(LDFLAGS)"
else
	$(WAILS) build -ldflags "$(LDFLAGS)"
endif

.PHONY: build-file-provider
build-file-provider: ## Signed macOS build with Finder access (requires Command Line Tools and signing configuration)
	bash scripts/macos/build-file-provider.sh --signed

ifeq ($(OS),Windows_NT)
clean: ## Remove build artifacts
	if exist "$(subst /,\,$(BUILD_DIR))" rd /s /q "$(subst /,\,$(BUILD_DIR))"
else
clean: ## Remove build artifacts
	rm -rf $(BUILD_DIR)
endif

bindings: ## Regenerate Wails frontend bindings after Go changes
ifeq ($(HOST_OS),Linux)
	bash scripts/linux/wails.sh generate module
else
	$(WAILS) generate module
endif

## Install

ifeq ($(OS),Windows_NT)
install: build ## Build and install (kills running instance)
	-taskkill /IM "$(APP_NAME).exe" /F 2>nul
	@rem Give Windows a moment to release the killed exe. Not "timeout": it
	@rem fails without a console (an IDE runner, CI), and from Git Bash cmd.exe
	@rem finds GNU timeout first; either way the recipe stopped here, with the
	@rem app killed and nothing installed.
	powershell -NoProfile -Command "Start-Sleep -Seconds 1"
	if not exist "$(INSTALL_DIR)" mkdir "$(INSTALL_DIR)"
	copy /Y "$(subst /,\,$(APP_EXE))" "$(INSTALL_DIR)\$(APP_NAME).exe"
	@echo Installed to $(INSTALL_DIR)\$(APP_NAME).exe
	-if exist "$(DATA_DIR)\$(APP_NAME).exe" del /F /Q "$(DATA_DIR)\$(APP_NAME).exe"
	@rem The paths are rebuilt from the environment inside PowerShell (same as
	@rem INSTALL_DIR and START_MENU_LINK) so a quote in them cannot break the command.
	powershell -NoProfile -Command "$$d=Join-Path $$env:LOCALAPPDATA 'Programs\$(APP_NAME)'; $$s=(New-Object -ComObject WScript.Shell).CreateShortcut((Join-Path $$env:APPDATA 'Microsoft\Windows\Start Menu\Programs\$(APP_NAME).lnk')); $$s.TargetPath=(Join-Path $$d '$(APP_NAME).exe'); $$s.WorkingDirectory=$$d; $$s.Save()"
	@echo Start Menu shortcut: $(START_MENU_LINK)
	if not exist "$(PLUGIN_DIR)" mkdir "$(PLUGIN_DIR)"
	copy plugins\*.lua "$(PLUGIN_DIR)\"
	@echo Updated bundled plugins
	start "" "$(INSTALL_DIR)\$(APP_NAME).exe"

uninstall: ## Remove installed app (keeps data)
	-taskkill /IM "$(APP_NAME).exe" /F 2>nul
	powershell -NoProfile -Command "Start-Sleep -Seconds 1"
	if exist "$(INSTALL_DIR)" rd /s /q "$(INSTALL_DIR)"
	-if exist "$(START_MENU_LINK)" del /F /Q "$(START_MENU_LINK)"
	-if exist "$(DATA_DIR)\$(APP_NAME).exe" del /F /Q "$(DATA_DIR)\$(APP_NAME).exe"
	-reg delete "HKCU\Software\Microsoft\Windows\CurrentVersion\Run" /v $(APP_NAME) /f 2>nul
	@echo Removed $(APP_NAME) from $(INSTALL_DIR); clips and settings in $(DATA_DIR) are kept
else ifeq ($(HOST_OS),Linux)
install: build ## Build and install the app and mp (with completions) to ~/.local (kills running instance)
	@# mp is pure Go; image-based hosts may only have Go inside the dev container.
	@# Built after 'build' (which cleans build/bin), never alongside it.
	@if command -v go >/dev/null 2>&1; then \
		$(MAKE) mp; \
	else \
		echo "Go not found on the host; installing without mp (run 'make mp-install' where Go is available)." >&2; \
	fi
	@bash scripts/linux/install.sh

uninstall: ## Remove installed app, mp and completions (keeps data)
	@bash scripts/linux/install.sh --uninstall
else
install: build ## Build and install (kills running instance)
	@# Wait for the old process to really exit. Replacing/opening the bundle while
	@# it is still shutting down makes LaunchServices focus the stale process.
	@$(MAKE) install-ready-bundle
	@echo "Installed to $(INSTALL_DIR)/$(APP_NAME).app"
	@echo "Updated bundled plugins"

install-ready-bundle:
	@$(MAKE) stop-installed-app
	@$(MAKE) install-bundle
	@# Keep the app from observing stale bundled plugins on its first launch.
	@mkdir -p "$(PLUGIN_DIR)"
	@cp plugins/*.lua "$(PLUGIN_DIR)/"
	@# Launching first makes LaunchServices register the just-installed app.
	@open -n $(INSTALL_DIR)/$(APP_NAME).app
	@sleep 1
	@# ExtensionKit can revive an old extension while the bundle is copied. Stop
	@# it after registration, so the next Finder request loads this app.
	@$(MAKE) stop-file-provider-extension

stop-installed-app:
	@pkill -TERM -f "$(APP_PROCESS_PATTERN)" 2>/dev/null || true
	@i=0; while [ $$i -lt 50 ]; do \
		if ! pgrep -f "$(APP_PROCESS_PATTERN)" >/dev/null 2>&1; then break; fi; \
		sleep 0.1; i=$$((i + 1)); \
	done; \
	if pgrep -f "$(APP_PROCESS_PATTERN)" >/dev/null 2>&1; then \
		pkill -KILL -f "$(APP_PROCESS_PATTERN)" 2>/dev/null || true; \
	fi
	@$(MAKE) stop-file-provider-extension

stop-file-provider-extension:
	@# ExtensionKit can outlive the app that owns it. Ensure a Finder request
	@# after installation starts the extension embedded in the replacement app.
	@pkill -TERM -f "$(FILE_PROVIDER_PROCESS_PATTERN)" 2>/dev/null || true
	@i=0; while [ $$i -lt 50 ]; do \
		if ! pgrep -f "$(FILE_PROVIDER_PROCESS_PATTERN)" >/dev/null 2>&1; then break; fi; \
		sleep 0.1; i=$$((i + 1)); \
	done; \
	if pgrep -f "$(FILE_PROVIDER_PROCESS_PATTERN)" >/dev/null 2>&1; then \
		pkill -KILL -f "$(FILE_PROVIDER_PROCESS_PATTERN)" 2>/dev/null || true; \
	fi

install-bundle:
	@set -eu; \
	staging="$(INSTALL_DIR)/.$(APP_NAME).installing.$$$$"; \
	target="$(INSTALL_DIR)/$(APP_NAME).app"; \
	previous="$(INSTALL_DIR)/.$(APP_NAME).previous.$$$$"; \
	cleanup() { \
		status="$$1"; \
		trap - EXIT HUP INT TERM; \
		set +e; \
		if [ -e "$$previous" ]; then \
			rm -rf "$$target"; \
			mv "$$previous" "$$target"; \
		fi; \
		rm -rf "$$staging"; \
		exit "$$status"; \
	}; \
	trap 'cleanup $$?' EXIT; \
	trap 'exit 1' HUP INT TERM; \
	rm -rf "$$staging"; \
	cp -R "$(APP_BUNDLE)" "$$staging"; \
	xattr -cr "$$staging"; \
	: "Do not replace the known-good installed app with an unsigned or corrupted build."; \
	codesign --verify --deep --strict "$$staging"; \
	if [ -e "$$target" ]; then mv "$$target" "$$previous"; fi; \
	if ! mv "$$staging" "$$target"; then \
		if [ -e "$$previous" ]; then mv "$$previous" "$$target"; fi; \
		exit 1; \
	fi; \
	if ! codesign --verify --deep --strict "$$target"; then \
		rm -rf "$$target"; \
		if [ -e "$$previous" ]; then mv "$$previous" "$$target"; fi; \
		exit 1; \
	fi; \
	: "The verified target is committed; backup cleanup cannot roll it back."; \
	trap - EXIT HUP INT TERM; \
	rm -rf "$$previous"

uninstall: ## Remove installed app
	@pkill -f "$(APP_PROCESS_PATTERN)" 2>/dev/null || true
	rm -rf $(INSTALL_DIR)/$(APP_NAME).app
	rm -f "$(HOME)/Library/LaunchAgents/io.github.egeozcan.mahpastes.login.plist"
	@echo "Removed $(APP_NAME) from $(INSTALL_DIR)"
endif

## CLI

mp: ## Build mp CLI for current platform
	go build -ldflags "$(LDFLAGS)" -o $(MP_BIN) ./cmd/mp

mp-install: mp ## Install mp to MP_INSTALL_DIR (user-writable by default)
ifeq ($(OS),Windows_NT)
	if not exist "$(MP_INSTALL_DIR)" mkdir "$(MP_INSTALL_DIR)"
	copy build\bin\mp.exe "$(MP_INSTALL_DIR)\mp.exe"
	@echo Installed mp to $(MP_INSTALL_DIR)\mp.exe
	@echo Add $(MP_INSTALL_DIR) to PATH to run mp from a new terminal.
else
	@mkdir -p "$(MP_INSTALL_DIR)"
	@BINDIR="$(MP_INSTALL_DIR)" bash scripts/install-mp.sh build/bin/mp
endif

mp-uninstall: ## Remove mp, its shell completions and man pages
ifeq ($(OS),Windows_NT)
	if exist "$(MP_INSTALL_DIR)\mp.exe" del "$(MP_INSTALL_DIR)\mp.exe"
else
	@BINDIR="$(MP_INSTALL_DIR)" bash scripts/install-mp.sh --uninstall
endif

mp-docs: mp ## Regenerate the CLI reference (docs/docs/reference/cli.md) and man pages (build/man)
	$(MP_BIN) gendocs --markdown docs/docs/reference/cli.md --man build/man

mp-cross: ## Cross-compile mp for all platforms
	$(call crossenv,GOOS=darwin GOARCH=amd64) go build -ldflags "$(LDFLAGS)" -o build/bin/mp-darwin-amd64 ./cmd/mp
	$(call crossenv,GOOS=darwin GOARCH=arm64) go build -ldflags "$(LDFLAGS)" -o build/bin/mp-darwin-arm64 ./cmd/mp
	$(call crossenv,GOOS=linux GOARCH=amd64) go build -ldflags "$(LDFLAGS)" -o build/bin/mp-linux-amd64 ./cmd/mp
	$(call crossenv,GOOS=linux GOARCH=arm64) go build -ldflags "$(LDFLAGS)" -o build/bin/mp-linux-arm64 ./cmd/mp
	$(call crossenv,GOOS=windows GOARCH=amd64) go build -ldflags "$(LDFLAGS)" -o build/bin/mp-windows-amd64.exe ./cmd/mp
	$(call crossenv,GOOS=windows GOARCH=arm64) go build -ldflags "$(LDFLAGS)" -o build/bin/mp-windows-arm64.exe ./cmd/mp

mahpastesd: ## Build headless server for current platform
	go build -ldflags "$(LDFLAGS)" -o $(MAHPASTESD_BIN) ./cmd/mahpastesd

mahpastesd-cross: ## Cross-compile headless server for all platforms (darwin with cgo only on a macOS host)
	$(call crossenv,GOOS=linux GOARCH=amd64) go build -ldflags "$(LDFLAGS)" -o build/bin/mahpastesd-linux-amd64 ./cmd/mahpastesd
	$(call crossenv,GOOS=linux GOARCH=arm64) go build -ldflags "$(LDFLAGS)" -o build/bin/mahpastesd-linux-arm64 ./cmd/mahpastesd
	$(call crossenv,CGO_ENABLED=0 GOOS=windows GOARCH=amd64) go build -ldflags "$(LDFLAGS)" -o build/bin/mahpastesd-windows-amd64.exe ./cmd/mahpastesd
	$(call crossenv,CGO_ENABLED=0 GOOS=windows GOARCH=arm64) go build -ldflags "$(LDFLAGS)" -o build/bin/mahpastesd-windows-arm64.exe ./cmd/mahpastesd
ifeq ($(HOST_OS),Darwin)
	@# On macOS build with cgo so the import wizard's Finder trash is
	@# recoverable; Go disables cgo for a foreign GOARCH unless asked, and the
	@# macOS clang builds either slice.
	$(call crossenv,CGO_ENABLED=1 GOOS=darwin GOARCH=amd64) go build -ldflags "$(LDFLAGS)" -o build/bin/mahpastesd-darwin-amd64 ./cmd/mahpastesd
	$(call crossenv,CGO_ENABLED=1 GOOS=darwin GOARCH=arm64) go build -ldflags "$(LDFLAGS)" -o build/bin/mahpastesd-darwin-arm64 ./cmd/mahpastesd
else
	@# Elsewhere the darwin server is built without cgo: everything works, but
	@# the import wizard deletes originals permanently instead of using the Trash.
	$(call crossenv,CGO_ENABLED=0 GOOS=darwin GOARCH=amd64) go build -ldflags "$(LDFLAGS)" -o build/bin/mahpastesd-darwin-amd64 ./cmd/mahpastesd
	$(call crossenv,CGO_ENABLED=0 GOOS=darwin GOARCH=arm64) go build -ldflags "$(LDFLAGS)" -o build/bin/mahpastesd-darwin-arm64 ./cmd/mahpastesd
endif

ifeq ($(HOST_OS),Linux)
mahpastesd-install: mahpastesd ## Linux: install mahpastesd and its systemd user unit (does not enable it)
	@mkdir -p "$(HOME)/.local/bin" "$(HOME)/.config/systemd/user"
	@install -m 755 build/bin/mahpastesd "$(HOME)/.local/bin/mahpastesd"
	@install -m 644 scripts/linux/mahpastesd.service "$(HOME)/.config/systemd/user/mahpastesd.service"
	@echo "Installed mahpastesd to $(HOME)/.local/bin and the user unit to $(HOME)/.config/systemd/user. Next:"
	@echo "  systemctl --user daemon-reload && systemctl --user enable --now mahpastesd"
else
mahpastesd-install:
	@echo "mahpastesd-install installs a systemd user unit and is Linux-only."
	@exit 1
endif

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
	@echo   install        Build and install to %LOCALAPPDATA%\Programs with a Start Menu shortcut
	@echo   uninstall      Remove the installed app and shortcut (keeps data)
	@echo   bindings       Regenerate Wails frontend bindings after Go changes
	@echo   test           Run e2e tests
	@echo   test-headed    Run e2e tests with visible browser
	@echo   test-debug     Run e2e tests with Playwright inspector
	@echo   screenshots    Capture documentation screenshots via Playwright
	@echo   mp             Build mp CLI for current platform
	@echo   mp-install     Install mp to MP_INSTALL_DIR
	@echo   mp-cross       Cross-compile mp for all platforms
	@echo   mahpastesd     Build headless server for current platform
	@echo   mahpastesd-cross Cross-compile headless server for all platforms
else
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-15s\033[0m %s\n", $$1, $$2}'
endif

.DEFAULT_GOAL := help
