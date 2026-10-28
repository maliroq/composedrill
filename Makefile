.PHONY: build test vet integration clean

build:
	go build -trimpath -buildvcs=false -o bin/composedrill ./cmd/composedrill

test:
	go test ./...

vet:
	go vet ./...

integration:
	go test -tags=integration -count=1 -v ./internal/integration

clean:
	rm -rf bin dist
