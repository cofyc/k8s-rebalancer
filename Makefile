GO := go
PKGS = $(shell $(GO) list ./... | grep -v /vendor/)

ARCH ?= $(shell go env GOARCH)
OS ?= $(shell go env GOOS)

ALL_TARGETS := cmd/k8s-rebalancer
SRC_PREFIX := github.com/cofyc/k8s-rebalancer

all: verify build test
.PHONY: all

build: $(ALL_TARGETS)
.PHONY: all

$(ALL_TARGETS):
	GOOS=$(OS) GOARCH=$(ARCH) CGO_ENABLED=0 $(GO) build -o output/bin/$(OS)/$(ARCH)/$@ $(SRC_PREFIX)/$@
.PHONY: $(ALL_TARGETS)

verify:
	./hack/verify-all.sh
.PHONY: verify

test: 
	$(GO) test $(PKGS)
.PHONY: test

e2e: 
	./hack/e2e.sh
.PHONY: e2e
