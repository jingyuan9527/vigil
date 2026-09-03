.PHONY: build run tidy fmt vet

build:
	cd backend && go build -o ../bin/dockmon ./cmd/server

run:
	cd backend && go run ./cmd/server

tidy:
	cd backend && go mod tidy

fmt:
	cd backend && gofmt -w .

vet:
	cd backend && go vet ./...
