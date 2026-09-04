.PHONY: build ui test vet fmt

build: ui
	CGO_ENABLED=0 go build -o bin/loomwork ./cmd/loomwork

ui:
	cd web/ui && npm ci && npm run build

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .
