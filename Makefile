build:
	go build -ldflags="-s -w" -o kbn ./cmd/kbn

install: build
	install -Dm755 kbn $(HOME)/.local/bin/kbn

test:
	go test ./...
