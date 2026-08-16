.PHONY: build build-verify run test docker lint clean

build:
	CGO_ENABLED=1 go build -o bin/agentgate ./cmd/agentgw

build-verify:
	CGO_ENABLED=1 go build -o bin/agentgate-verify ./cmd/agentgate-verify

run:
	go run ./cmd/agentgw

test:
	go test -v -count=1 ./...

docker:
	docker-compose up --build

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/
