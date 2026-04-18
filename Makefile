GO ?= go
GOFLAGS ?=
PKGS ?= ./...
BINDIR ?= bin

.PHONY: all build vet test tidy clean fmt check

all: check build

build:
	mkdir -p $(BINDIR)
	$(GO) build $(GOFLAGS) -o $(BINDIR)/hubert-runner ./cmd/hubert-runner
	$(GO) build $(GOFLAGS) -o $(BINDIR)/hubert-dispatch ./cmd/hubert-dispatch
	$(GO) build $(GOFLAGS) -o $(BINDIR)/hubert-snap ./cmd/hubert-snap
	$(GO) build $(GOFLAGS) -o $(BINDIR)/hubert-prompts ./cmd/hubert-prompts

vet:
	$(GO) vet $(PKGS)

test:
	$(GO) test $(GOFLAGS) $(PKGS)

tidy:
	$(GO) mod tidy

fmt:
	$(GO) fmt $(PKGS)

check: vet test

clean:
	rm -rf $(BINDIR)
