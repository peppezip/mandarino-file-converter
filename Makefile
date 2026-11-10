# Common tasks. Everything is plain Go plus two shell scripts.
VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo dev)

.PHONY: all test vet deps build windows linux screenshots demo icon clean

all: vet test build

## Tests (needs ffmpeg in PATH; run "make deps" first to enable the PDF tests)
test:
	MANDARINO_TOOLS=$(CURDIR)/third_party/linux go test ./...

## Static checks for both platforms
vet:
	go vet -unsafeptr=false ./...
	GOOS=windows GOARCH=amd64 go vet -unsafeptr=false ./...

## Download ffmpeg and pdfium for both platforms into third_party/
deps:
	scripts/fetch-deps.sh all

## Build and package both platforms into dist/
build:
	scripts/build.sh $(VERSION) all

windows:
	scripts/build.sh $(VERSION) windows

linux:
	scripts/build.sh $(VERSION) linux

## Regenerate docs/screenshots (Linux only, uses cairo)
screenshots:
	go run . --screenshots docs/screenshots

## Regenerate docs/demo: README animations (GIF + MP4) and the social preview (Linux, needs ffmpeg)
demo:
	go run . --demo docs/demo

## Regenerate the Windows icon/manifest resource (needs Python + Pillow)
icon:
	python3 scripts/mkrsrc.py

clean:
	rm -rf dist
