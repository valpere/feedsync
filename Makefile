.PHONY: build test vet lint gen-sample convert bench mysql-up mysql-down mysql-seed mysql-sync

DSN ?= root:demo@tcp(127.0.0.1:3307)/opencart

build:
	go build -o bin/feedsync ./cmd/feedsync

test:
	go test ./... -race -cover

vet:
	go vet ./...

lint: vet
	gofmt -l .

# 25,000-offer sample supplier feed (deterministic)
gen-sample: build
	./bin/feedsync gen-sample -n 25000 -seed 1 -o testdata/generated/supplier.yml

convert: build
	./bin/feedsync convert -c examples/config.yaml

# wall time and peak RSS for a full conversion (needs GNU time)
bench: build gen-sample
	for i in 1 2 3; do /usr/bin/time -f "%e s wall, %M KB peak RSS" ./bin/feedsync convert -c examples/config.yaml >/dev/null; done

mysql-up:
	docker compose up -d --wait mysql

mysql-down:
	docker compose down -v

mysql-seed: build
	./bin/feedsync opencart-seed -dsn '$(DSN)' -n 25000

mysql-sync: build
	./bin/feedsync convert -c examples/config.yaml -opencart-dsn '$(DSN)'
