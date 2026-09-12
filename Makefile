# wrk3 — install, build, test, release.
#
#   make build      compile ./bin/wrk3 (stamped with version)
#   make install    install to ~/.local/bin (no sudo needed)
#   make install-user same as install (alias)
#   make uninstall  remove installed binary + completions
#   machine-wide instead: sudo make install PREFIX=/usr/local
#   make test       go vet + go test
#   make lint       golangci-lint (if installed)
#   make completion generate shell completions into ./completions
#   make snapshot   goreleaser dry-run (no publish)
#   make release    goreleaser publish (needs GITHUB_TOKEN + git tag)
#   make clean      remove build artifacts

MODULE      := github.com/mytmlt/wrk3
BIN         := wrk3
BINDIR      := bin
# Default is user-local (~/.local/bin) so plain `make install` needs no sudo.
# Machine-wide install (all users): sudo make install PREFIX=/usr/local
PREFIX      ?= $(HOME)/.local
DESTDIR     ?=
INSTALL_DIR := $(DESTDIR)$(PREFIX)/bin

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X $(MODULE)/cmd.Version=$(VERSION) \
	-X $(MODULE)/cmd.Commit=$(COMMIT) \
	-X $(MODULE)/cmd.Date=$(DATE)

GO      ?= go
GORELEASER ?= goreleaser

.PHONY: all build install install-user uninstall test vet lint completion snapshot release clean help

all: build

build: ## Compile ./bin/wrk3 with version stamp
	@mkdir -p $(BINDIR)
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINDIR)/$(BIN) .

install: build ## Install wrk3 to ~/.local/bin (no sudo; PREFIX overridable)
	install -d $(INSTALL_DIR)
	install -m 0755 $(BINDIR)/$(BIN) $(INSTALL_DIR)/$(BIN)
	@echo "installed $(INSTALL_DIR)/$(BIN) ($(VERSION))"
	@echo "note: ensure $(INSTALL_DIR) is on PATH (see docs/INSTALL.md)"

install-user: ## Alias for install (user-local, no sudo needed)
	@$(MAKE) install PREFIX=$(HOME)/.local

uninstall: ## Remove installed wrk3 from $(PREFIX)/bin
	rm -f $(INSTALL_DIR)/$(BIN)
	@echo "removed $(INSTALL_DIR)/$(BIN)"

test: ## go vet + full test suite
	$(GO) vet ./...
	$(GO) test ./... -count=1

vet: ## go vet only
	$(GO) vet ./...

lint: ## golangci-lint if available
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed (see https://golangci-lint.run/docs/welcome/install/)"; \
	fi

completion: build ## Generate bash/zsh/fish/powershell completions
	@mkdir -p completions
	@for sh in bash zsh fish powershell; do \
		./$(BINDIR)/$(BIN) completion $$sh > completions/$(BIN).$$sh; \
		echo "wrote completions/$(BIN).$$sh"; \
	done

snapshot: ## goreleaser --snapshot (local artifact test, no publish)
	$(GORELEASER) release --snapshot --clean

release: ## goreleaser publish (requires GITHUB_TOKEN and a git tag like v0.1.0)
	$(GORELEASER) release --clean

clean: ## Remove build artifacts
	rm -rf $(BINDIR) dist completions

help: ## Show targets
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'
