.PHONY: build build-verify run test docker lint clean

build:
	CGO_ENABLED=0 go build -o bin/agentgate ./cmd/agentgw

build-verify:
	CGO_ENABLED=0 go build -o bin/agentgate-verify ./cmd/agentgate-verify

run:
	CGO_ENABLED=0 go run ./cmd/agentgw

test:
	CGO_ENABLED=0 go test -v -count=1 ./...

docker:
	docker-compose up --build

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/
