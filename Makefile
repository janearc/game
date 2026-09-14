# game. the same verbs as the other libraries:
#   make          gofmt, go vet, go test
#   make build    bin/game
#   make clean    bin/ away
#   make release  game cuts its own release to PUBLIC, using itself
PUBLIC ?= ../../game-root.git
.PHONY: all fmt vet test build clean release
all: fmt vet test
fmt:
	gofmt -l -w .
vet:
	go vet ./...
test:
	go test ./...
build:
	@mkdir -p bin
	@go build -ldflags "-X main.build=$$(git rev-parse --short HEAD) -X main.built=$$(date -u +%Y-%m-%dT%H:%M:%SZ)" -o bin/game ./cmd/game
clean:
	rm -rf bin
release: build
	@bin/game release --public $(PUBLIC)
