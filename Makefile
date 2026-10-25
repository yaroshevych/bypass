GO ?= go
INSTALL ?= install
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin
DESTDIR ?=
BINARY := bin/bypass

.PHONY: all build install

all: build

build:
	mkdir -p bin
	$(GO) build -trimpath -o $(BINARY) ./cmd/bypass

install: build
	$(INSTALL) -d "$(DESTDIR)$(BINDIR)"
	$(INSTALL) -m 0755 $(BINARY) "$(DESTDIR)$(BINDIR)/bypass"
