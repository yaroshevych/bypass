GO ?= go
INSTALL ?= install
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
DESTDIR ?=
BINARY := bin/bypass
VERSION ?= dev
LDFLAGS ?= -X main.version=$(VERSION)

export GOOS GOARCH

.PHONY: all build install

all: build

build:
	mkdir -p bin
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/bypass

install: build
	$(INSTALL) -d "$(DESTDIR)$(BINDIR)"
	$(INSTALL) -m 0755 $(BINARY) "$(DESTDIR)$(BINDIR)/bypass"
