.PHONY: fmt vet test build ci

fmt:
	gofmt -w $$(find . -type f -name '*.go' -not -path './vendor/*')

vet:
	go vet ./...

test:
	go test ./...

build:
	go build ./...

ci: fmt vet test build


safe-commit:
	./scripts/safe-commit.sh "$(MSG)"
