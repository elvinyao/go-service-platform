# Code Style & Conventions

## General Go Conventions
- Standard Go formatting (gofmt)
- Package names are lowercase, single-word
- Module path is `project` (not a URL-based path)

## Naming
- Exported types use PascalCase: `BaseService`, `WorkflowManager`
- Unexported fields use camelCase: `name`, `serviceType`, `running`
- Mutex fields: `mu sync.RWMutex` or `lock sync.Mutex`
- Interface names end with descriptive noun: `Service`, `Workflow`, `Checker`, `DataAccessor`
- Service names are descriptive strings: `"WebSocketServiceA"`, `"ConfluenceServiceA"`, `"BadgeDBService"`

## Comments
- Exported functions/types have Go-doc style comments: `// FuncName does X`
- Brief, English comments (project recently switched to English comments)
- No excessive commenting on internal logic

## Error Handling
- Custom error types via `pkg/errors` package with error types like `TypeNotFound`, `TypeInternal`, `TypeInvalidInput`, `TypeServiceUnavailable`
- Errors created with `errors.New(errorType, message, cause)` or `errors.Wrap(err, message, errorType)`
- Error fields via `.WithField(key, value)` chaining

## Logging
- Always use context-aware logging: `logger.InfoWithContext(ctx, "message")`
- Use `logger.LogOperation(ctx, "operation_name", func)` for timed operations
- Use `logger.WithContextError(ctx, err).Error("message")` for errors
- Log fields via `logger.WithContextFields(ctx, logrus.Fields{...})`

## Context
- Application context via `appctx` (alias for `project/pkg/context`)
- Always propagate context: `appctx.WithOperationName(ctx, "op")`, `appctx.WithServiceName(ctx, name)`
- New request contexts via `appctx.NewContext(parentCtx)`

## Service Pattern
- All services embed `BaseService` for common lifecycle functionality
- Implement `service.Service` interface: `Start`, `Stop`, `GetName`, `IsRunning`
- Use `sync.RWMutex` for thread safety
- Services are registered with the `ServiceManager`

## Workflow Pattern
- Workflows implement `interfaces.Workflow` interface
- Created via factory functions in `internal/workflow/factories.go`
- Registered with `WorkflowManager` via DI container

## Package Structure
- `internal/` for private application code
- `pkg/` for reusable library code
- `cmd/` for entry points
- `config/` for configuration files and providers
- Interfaces defined in `internal/interfaces/`
- Models in `internal/model/`
