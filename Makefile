.PHONY: build test clean

build:
	go build -o bin/orch ./cmd/orch/

test:
	go test ./... -v -cover

vet:
	go vet ./...

clean:
	rm -rf bin/
