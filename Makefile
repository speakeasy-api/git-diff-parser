.PHONY: lint fmt test

lint:
	golangci-lint run

fmt:
	gofumpt -l -w .

test:
	go test -race -count=1 ./...
