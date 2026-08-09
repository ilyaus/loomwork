.PHONY: build test vet fmt

build:
	CGO_ENABLED=0 go build -o bin/loomwork ./cmd/loomwork

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .
