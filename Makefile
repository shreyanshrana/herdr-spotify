VERSION ?= 0.1.0

.PHONY: build test vet fmt-check check package clean

build:
	CGO_ENABLED=0 go build -trimpath -o bin/herdr-spotify .
	CGO_ENABLED=0 go build -trimpath -o bin/herdr-spotify-launcher ./cmd/launcher

test:
	go test -race ./...

vet:
	go vet ./...

fmt-check:
	@files="$$(gofmt -l *.go cmd/launcher/*.go)"; if [ -n "$$files" ]; then printf 'Run gofmt on:\n%s\n' "$$files"; exit 1; fi

check: fmt-check test vet

package:
	sh scripts/package.sh "$(VERSION)"

clean:
	rm -rf bin dist
