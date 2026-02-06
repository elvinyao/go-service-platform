# Suggested Commands

## Build & Run
```bash
# Build main application
go build -o bin/service-workflow cmd/main.go

# Build fake API servers
go build -o bin/fakeapi cmd/fakeapi/main.go

# Run with environment variables
LOG_LEVEL=debug LOG_FORMAT=json ./bin/service-workflow

# Run directly without building
go run cmd/main.go
go run cmd/fakeapi/main.go
```

## Testing
```bash
# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run tests for a specific package
go test -v ./pkg/di/...
go test -v ./internal/service/...
go test -v ./pkg/logger/...

# Run tests with coverage
go test -cover ./...
go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out
```

## Code Quality
```bash
# Format code
gofmt -w .

# Vet code
go vet ./...

# Tidy modules
go mod tidy
```

## Dependency Management
```bash
# Download dependencies
go mod download

# Tidy (remove unused, add missing)
go mod tidy

# Verify dependencies
go mod verify
```

## System Utilities (macOS / Darwin)
```bash
# Git
git status
git log --oneline -20
git diff
git branch

# File operations
ls -la
find . -name "*.go" -not -path "./.git/*"

# Process management
lsof -i :8080   # check if port is in use
```
