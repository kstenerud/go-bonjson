# Makefile for go-bonjson

.PHONY: all test test-verbose bench cover cover-html clean

# Default target runs all tests with coverage summary
all: test

# Run all tests (Go unit tests + spec tests via TestBONJSONSpec)
test:
	@echo "=== Running all tests ==="
	go test -cover .

# Run tests with verbose output
test-verbose:
	@echo "=== Running all tests (verbose) ==="
	go test -v -cover .

# Run benchmarks
bench:
	go test -bench=. -benchmem .

# Run tests with coverage report
cover:
	go test -coverprofile=coverage.out .
	go tool cover -func=coverage.out

# Generate HTML coverage report
cover-html: cover
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Clean build artifacts
clean:
	rm -f coverage.out coverage.html
