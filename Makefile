.PHONY: build test e2e e2e-upgrade vet fmt lint clean

# Build the sea binary to bin/sea (matches what e2e tests expect)
build:
	go build -o bin/sea ./cmd/sea

# Run unit tests with race detection
test:
	go test -race ./... -count=1

# Run the main end-to-end test (requires: cc, c++, cmake, curl, make)
e2e: build
	./examples/run-e2e.sh

# Run the upgrade end-to-end test
e2e-upgrade: build
	./examples/run-upgrade-e2e.sh

# Run go vet
vet:
	go vet ./...

# Check formatting
fmt:
	@test -z "$$(gofmt -l .)" || (echo "Files need formatting:"; gofmt -l .; exit 1)

# Run all checks (vet + fmt + test)
lint: vet fmt test

# Remove build artifacts
clean:
	rm -rf bin/ dist/
