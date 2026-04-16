.PHONY: build test lint cover docker run-matrix run-direct

build:
	go build ./...

test:
	go test -race -count=1 -coverprofile=coverage.out ./...

cover: test
	go tool cover -func=coverage.out | tail -1

lint:
	go vet ./...
	golangci-lint run

docker:
	docker build -t action-deployer:dev .

# Local dry-run: matrix mode
run-matrix:
	INPUT_SERVICE=ts-spa INPUT_VERSION=2026.04.14.5 INPUT_SHA=abc1234 \
	  INPUT_TOKEN=x INPUT_DRY_RUN=true \
	  go run ./cmd/action-deployer

# Local dry-run: direct mode
run-direct:
	INPUT_FILE=charts/my-app/values.yaml INPUT_VALUE=2.0.0 \
	  INPUT_TOKEN=x INPUT_DRY_RUN=true \
	  go run ./cmd/action-deployer
