.PHONY: fmt vet test build ci lint clean

fmt:
	gofmt -w $$(find . -type f -name '*.go' -not -path './vendor/*')

vet:
	go vet ./...

lint:
	golangci-lint run ./...

test:
	go test -v -count=1 ./...

test-cover:
	go test -v -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

build:
	go build ./...

ci: fmt vet lint test build

clean:
	rm -f coverage.out
	rm -rf dist/

safe-commit:
	./scripts/safe-commit.sh "$(MSG)"
