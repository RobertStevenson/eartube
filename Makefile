.PHONY: tidy build run
tidy:
	go mod tidy
build:
	go build -o bin/eartube ./cmd/eartube
run:
	go run ./cmd/eartube
