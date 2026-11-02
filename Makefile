.PHONY: tidy build run test test-integration
tidy:
	go mod tidy
build:
	go build -o bin/eartube ./cmd/eartube
run:
	go run ./cmd/eartube $(ARGS)

# Fast and offline.
test:
	go test -race ./...

# Real mpv, and real YouTube via yt-dlp. Add -short to skip the network tests.
# Set EARTUBE_BROWSER (e.g. brave+gnomekeyring) to include the signed-in playlist test.
test-integration:
	go test -tags integration -race ./internal/player ./internal/ytdlp
