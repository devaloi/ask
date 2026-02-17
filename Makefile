.PHONY: build run test lint fmt clean install

# Build the binary
build:
	go build -o ask .

# Run the application
run:
	go run .

# Run all tests
test:
	go test -race -v ./...

# Run tests with coverage
coverage:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run linter
lint:
	golangci-lint run

# Format code
fmt:
	gofmt -s -w .
	goimports -w -local github.com/devaloi/ask .

# Clean build artifacts
clean:
	rm -f ask coverage.out coverage.html

# Install the binary
install:
	go install .

# Run go vet
vet:
	go vet ./...

# Run all checks (lint + test + vet)
check: lint vet test
