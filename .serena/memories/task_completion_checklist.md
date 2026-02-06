# Task Completion Checklist

When a coding task is completed, run the following checks:

## 1. Code Compiles
```bash
go build ./...
```

## 2. Tests Pass
```bash
go test ./...
```

## 3. Code Formatting
```bash
gofmt -l .
# If any files listed, format them:
gofmt -w .
```

## 4. Vet Check
```bash
go vet ./...
```

## 5. Module Tidy (if dependencies changed)
```bash
go mod tidy
```

## Notes
- No Makefile exists in this project; use `go` commands directly
- No CI/CD configuration found in the repo
- No linter configuration (e.g., golangci-lint) found; standard `go vet` is sufficient
- The language server (gopls) is not initialized in the Serena environment, so symbolic tools may not work. Use file-based tools as fallback.
