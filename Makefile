.PHONY: build build-verify run test docker lint clean bench-e2e bench-e2e-open-loop bench-proc

build:
	CGO_ENABLED=0 go build -o bin/agentgate ./cmd/agentgw

build-verify:
	CGO_ENABLED=0 go build -o bin/agentgate-verify ./cmd/agentgate-verify

run:
	CGO_ENABLED=0 go run ./cmd/agentgw

test:
	CGO_ENABLED=0 go test -v -count=1 ./...

BENCHTIME ?= 20000x
BENCH_FILTER ?= ^BenchmarkHandleActE2E$$
BENCH_PROC_ARGS ?=

bench-proc:
	mkdir -p .cache/tmp .cache/go-tmp .cache/go-build .cache/proc/bin
	TMPDIR="$(CURDIR)/.cache/tmp" GOTMPDIR="$(CURDIR)/.cache/go-tmp" \
		GOCACHE="$(CURDIR)/.cache/go-build" go build -o .cache/proc/bin/ ./bench/procgateway ./bench/mockupstream ./bench/loadgen
	python3 bench/proc.py $(BENCH_PROC_ARGS)

bench-e2e-open-loop: BENCH_FILTER = ^BenchmarkHandleActOpenLoop$$

bench-e2e bench-e2e-open-loop:
	mkdir -p .cache/tmp .cache/go-tmp .cache/go-build
	TMPDIR="$(CURDIR)/.cache/tmp" GOTMPDIR="$(CURDIR)/.cache/go-tmp" \
		GOCACHE="$(CURDIR)/.cache/go-build" GOMAXPROCS=4 \
		go test -tags=bench ./bench -run '^$$' -bench '$(BENCH_FILTER)' -benchtime=$(BENCHTIME) -count=1

docker:
	docker-compose up --build

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/
