GO ?= go
GOOS ?= $(shell $(GO) env GOOS)
GOARCH ?= $(shell $(GO) env GOARCH)

BINARY := opfor$(if $(filter windows,$(GOOS)),.exe)

.PHONY: all
all:
	GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build -o ./$(BINARY) ./cmd/opfor
