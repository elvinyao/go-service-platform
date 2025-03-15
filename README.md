# Go Service Platform

A microservice workflow platform built with Go, designed for handling and coordinating message passing and business processes between different services.

## System Architecture

### Directory Structure
```
.
├── cmd/            # Main program entry point
├── internal/       # Internal packages
│   ├── config/      # Configuration definitions
│   ├── connections/ # Connection management
│   ├── dataaccess/  # Data access layer
│   ├── interfaces/  # Interface definitions
│   ├── manager/     # Manager components
│   ├── messaging/   # Message processing
│   ├── model/       # Data models
│   ├── service/     # Service implementations
│   └── workflow/    # Workflow definitions
├── pkg/            # Public packages
│   ├── context/     # Context utilities
│   ├── di/          # Dependency injection
│   ├── errors/      # Error handling
│   ├── health/      # Health checking
│   └── logger/      # Logging components
├── config/         # Configuration files
├── bin/            # Compilation output directory
└── .vscode/        # VSCode configuration
```

### System Architecture Diagram

```
+----------------------+     +-------------------------+     +----------------------+
|                      |     |                         |     |                      |
|  WebSocket Service   +---->+   Workflow Manager      +---->+  Confluence Service  |
|                      |     |                         |     |                      |
+----------------------+     +-------------+-----------+     +----------+-----------+
         ^                                 |                            |
         |                                 |                            |
         v                                 v                            v
+--------+-----------+     +--------------+----------+     +------------+---------+
|                    |     |                         |     |                      |
| Mattermost Service |     |    Service Manager      |     |  Cache Data Accessor |
|                    |     |                         |     |                      |
+--------------------+     +-------------+-----------+     +----------------------+
                                         |
                                         |
                                         v
                           +-------------+-----------+
                           |                         |
                           |    BadgeDB Service      |
                           |                         |
                           +-------------------------+
```

### Core Components

1. **Service Manager (ServiceManager)**
   - Responsible for service registration, startup, shutdown, and monitoring
   - Provides service discovery and retrieval functionality
   - Supports service health checks and automatic restart
   - Manages graceful startup and shutdown sequences

2. **Workflow Manager (WorkflowManager)**
   - Manages different workflows
   - Handles message dispatching and routing
   - Coordinates interactions between different services
   - Provides workflow listing and retrieval functionality

3. **Data Access Layer (DataAccessor)**
   - Provides a unified data access interface
   - Implements caching mechanisms
   - Supports data storage and retrieval operations

4. **Logging System**
   - Implemented with structured JSON logging
   - Supports context-aware logging with request IDs and tracing
   - Configurable via environment variables
   - Supports file rotation and multiple output destinations
   - Provides operation timing and error details

5. **Context Package**
   - Extends standard Go context with application-specific fields
   - Supports request ID, trace ID, user ID, and operation tracking
   - Enables context propagation throughout the application
   - Provides timeout and deadline handling

6. **Health Monitoring**
   - Provides health check endpoints for services
   - Reports service status and performance metrics
   - Supports both critical and non-critical dependency checks

### Component Relationships

1. **WebSocket Service** 
   - Receives external messages and forwards them to the workflow manager
   - Integrates with the workflow manager through callback mechanisms

2. **Mattermost Service**
   - Integrates with Mattermost systems
   - Provides real-time message processing
   - Communicates with Mattermost servers via Websocket

3. **Workflow Manager**
   - Registers and manages multiple workflows
   - Dispatches messages to appropriate workflow processors
   - Accesses other services through the service manager

4. **Service Manager**
   - Centrally manages the lifecycle of all services
   - Provides service discovery functionality
   - Monitors service health and automatically restarts services
   - Coordinates graceful shutdown sequence

5. **Cache Data Accessor**
   - Provides caching support for Confluence services
   - Implements high-performance cache
   - Isolates cache implementation details through interfaces

6. **BadgeDB Service**
   - Global service shared by all workflows
   - Provides method query functionality
   - Communicates with workflows through the service manager

## Business Architecture

### Business Architecture Diagram

```
+-------------------+     +-----------------+     +-------------------+
|                   |     |                 |     |                   |
|  External Client  +---->+  WebSocket Svc  +---->+  Mattermost Svc   |
|                   |     |                 |     |                   |
+-------------------+     +--------+--------+     +-------------------+
                                   |
                                   | Messages
                                   v
+-------------------+     +--------+--------+     +-------------------+
|                   |     |                 |     |                   |
|  Confluence System+<----+   Workflow A    +---->+  BadgeDB Database |
|                   |     |                 |     |                   |
+-------------------+     +-----------------+     +-------------------+
        ^
        |
        |
+-------+----------+
|                  |
|  Cache Data      |
|  Access Layer    |
+------------------+
```

### Message Flow
1. WebSocket or Mattermost service receives external messages
2. Messages are forwarded to the workflow manager
3. The workflow manager selects appropriate workflows based on message type
4. Workflows process messages and coordinate related services
5. All operations are logged with context information and timing metrics

### Workflow Processing Flow
1. **Message Reception**
   - WebSocket/Mattermost service receives external messages
   - Messages are passed to the workflow manager via callbacks
   - The workflow manager dispatches messages to registered workflows

2. **Message Processing (WorkflowA example)**
   - Get BadgeDB service to query processing method for message type
   - Find corresponding Confluence service based on processing method
   - Call Confluence service to get data
   - Record processing results to logs with contextual information

3. **Data Caching**
   - Confluence service uses cache data access layer to store data
   - Cache supports high-performance read/write operations
   - Cached data has expiration time with automatic cleanup

### Configuration Management
- Configuration structures divided into specific service configuration objects
- Each service's configuration passed through interfaces for type safety
- Support for dynamic configuration updates (via Configure method)

## Advanced Features

### Structured Logging

The platform includes a comprehensive structured logging system with the following features:

- **JSON Formatted Logs**: All logs are in JSON format by default for better parsing and indexing in log management systems
- **Context-aware Logging**: Logs include request IDs, trace IDs, and other contextual information
- **Configurable Log Levels**: Easily configure log levels via environment variables
- **File Rotation**: Support for log file rotation based on size and time
- **Multiple Outputs**: Logs can be sent to multiple destinations simultaneously
- **Performance Metrics**: Operations automatically include duration metrics
- **Error Details**: Enhanced error logging with structured error details

Environment variables for configuring logging:

- `LOG_LEVEL`: Set the log level (debug, info, warn, error, fatal, panic, trace)
- `LOG_FORMAT`: Log format (json or text)
- `LOG_TIME_FORMAT`: Timestamp format
- `LOG_CALLER_INFO`: Include caller information (true/false)
- `LOG_OUTPUT`: Output destination (stdout, stderr, or file path)

### Graceful Shutdown

The platform includes a robust graceful shutdown mechanism:

- **Signal Handling**: Properly handles SIGINT, SIGTERM, and SIGQUIT signals
- **Shutdown Sequence**: Implements a coordinated shutdown sequence to ensure resources are released properly
- **Timeout Handling**: Enforces timeouts during shutdown to prevent hanging
- **Resource Cleanup**: Ensures all resources (servers, connections, goroutines) are properly cleaned up
- **Contextual Shutdown**: Uses contexts to propagate shutdown signals throughout the application

The shutdown sequence follows this order:

1. Stop monitoring services first to avoid log spam during shutdown
2. Shutdown HTTP servers with proper timeouts
3. Stop all application services with graceful termination
4. Release resources and complete final logging

## Developer Guide

### Getting Started

#### Prerequisites
- Go 1.22.5 or higher
- VSCode (optional, configured with debugging settings)

#### Building and Running
```bash
# Build the application
go build -o bin/service-workflow cmd/main.go

# Run with environment variables
LOG_LEVEL=debug LOG_FORMAT=json ./bin/service-workflow
```

### Creating a New Service

1. **Define the service interface** in `internal/interfaces/`:
   ```go
   type MyServiceInterface interface {
       DoSomething(ctx context.Context, param string) (string, error)
   }
   ```

2. **Implement the service** in `internal/service/`:
   ```go
   type MyService struct {
       name      string
       running   bool
       config    *MyServiceConfig
       lock      sync.Mutex
   }

   func NewMyService(config *MyServiceConfig) *MyService {
       return &MyService{
           name:    "MyService",
           config:  config,
           running: false,
       }
   }

   // Implement Service interface methods
   func (s *MyService) Start(ctx context.Context) error {
       logger.InfoWithContext(ctx, "Starting MyService")
       s.lock.Lock()
       defer s.lock.Unlock()
       s.running = true
       return nil
   }

   func (s *MyService) Stop(ctx context.Context) error {
       logger.InfoWithContext(ctx, "Stopping MyService")
       s.lock.Lock()
       defer s.lock.Unlock()
       s.running = false
       return nil
   }

   func (s *MyService) GetName() string {
       return s.name
   }

   func (s *MyService) IsRunning(ctx context.Context) bool {
       return s.running
   }

   // Implement custom service methods
   func (s *MyService) DoSomething(ctx context.Context, param string) (string, error) {
       return logger.LogOperation(ctx, "do_something", func(opCtx context.Context) error {
           // Implementation goes here
           return nil
       })
   }
   ```

3. **Register the service** in the service manager:
   ```go
   // In internal/manager/service_manager.go or appropriate registration location
   myServiceConfig := config.GetMyServiceConfig()
   myService := service.NewMyService(myServiceConfig)
   serviceManager.RegisterService(myService)
   ```

### Creating a New Workflow

1. **Implement the Workflow interface** in `internal/workflow/`:
   ```go
   type MyWorkflow struct {
       name           string
       serviceManager *manager.ServiceManager
   }

   func NewMyWorkflow(serviceManager *manager.ServiceManager) *MyWorkflow {
       return &MyWorkflow{
           name:           "MyWorkflow",
           serviceManager: serviceManager,
       }
   }

   func (w *MyWorkflow) GetName() string {
       return w.name
   }

   func (w *MyWorkflow) ProcessMessage(ctx context.Context, msg model.Message) error {
       ctx = appctx.WithOperationName(ctx, "process_message")
       logger.InfoWithContext(ctx, "Processing message in MyWorkflow")

       // Process message based on type
       switch msg.Type {
       case "type1":
           return w.handleType1Message(ctx, msg)
       case "type2":
           return w.handleType2Message(ctx, msg)
       default:
           return errors.New(errors.TypeInvalidInput, "Unsupported message type", nil)
       }
   }

   func (w *MyWorkflow) handleType1Message(ctx context.Context, msg model.Message) error {
       // Implementation goes here
       return nil
   }
   ```

2. **Create a workflow factory** in `internal/workflow/factories.go`:
   ```go
   func CreateMyWorkflow(serviceManager *manager.ServiceManager) (interfaces.Workflow, error) {
       return NewMyWorkflow(serviceManager), nil
   }

   func RegisterWorkflowFactories() []manager.WorkflowFactory {
       return []manager.WorkflowFactory{
           CreateWorkflowA,
           CreateMyWorkflow, // Add your new workflow factory
       }
   }
   ```

3. **Register the workflow** with the workflow manager:
   ```go
   // This typically happens in the DI container setup
   workflowManager, err := manager.NewWorkflowManagerWithDI(
       ctx, 
       serviceManager, 
       workflow.RegisterWorkflowFactories()...
   )
   ```

### Using Structured Logging

```go
// Basic logging with context
logger.InfoWithContext(ctx, "Operation started")

// Logging with additional fields
logger.WithContextFields(ctx, logrus.Fields{
    "customer_id": customerId,
    "operation":   "data_sync",
}).Info("Syncing customer data")

// Logging errors with context
if err != nil {
    logger.WithContextError(ctx, err).Error("Failed to process data")
}

// Timing operations with automatic logging
err := logger.LogOperation(ctx, "database_query", func(opCtx context.Context) error {
    // Operation implementation
    results, err := db.Query(opCtx, queryString)
    return err
})

// Timing operations with minimal logging (only duration)
err := logger.LogTimingOperation(ctx, "cache_lookup", func(opCtx context.Context) error {
    // Operation implementation
    return nil
})
```

### Implementing Graceful Shutdown

For services that need custom shutdown logic:

```go
func (s *MyService) Stop(ctx context.Context) error {
    logger.InfoWithContext(ctx, "Stopping MyService gracefully")
    
    // Create a done channel to signal completion
    done := make(chan struct{})
    
    // Perform cleanup in a goroutine
    go func() {
        // Close connections
        s.closeConnections()
        
        // Finish pending operations
        s.finishPendingOperations()
        
        // Signal completion
        close(done)
    }()
    
    // Wait for completion or timeout
    select {
    case <-done:
        logger.InfoWithContext(ctx, "MyService stopped successfully")
    case <-ctx.Done():
        logger.WarnWithContext(ctx, "MyService shutdown timed out")
    }
    
    s.running = false
    return nil
}
```

### Error Handling Best Practices

```go
// Creating typed errors
if userID == "" {
    return errors.New(errors.TypeInvalidInput, "User ID cannot be empty", nil)
}

// Adding fields to errors
if err != nil {
    return errors.Wrap(err, "Failed to fetch user data", errors.TypeServiceUnavailable).
        WithField("user_id", userID).
        WithField("source", "database")
}

// Handling errors with proper logging
if err != nil {
    logger.WithContextError(ctx, err).Error("Operation failed")
    // Handle the error based on type
    switch errors.GetErrorType(err) {
    case errors.TypeNotFound:
        // Handle not found
    case errors.TypeServiceUnavailable:
        // Handle service unavailable
    default:
        // Handle other errors
    }
}
```

## Technology Stack

- Go 1.22.5+
- Dependency Management: Go Modules
- Logging: Logrus v1.9.3
- Context Management: Custom implementation

## Service Interface Standard

All services implement a unified Service interface, including the following methods:

```go
type Service interface {
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
    GetName() string
    IsRunning(ctx context.Context) bool
    // Additional methods may be required by specific service types
}
```

This platform is designed for high extensibility and modularity. Through interface definitions and dependency injection, components are loosely coupled, making them easy to test and maintain. The monitoring and error handling mechanisms are robust, ensuring stable service operation. The structured logging system provides comprehensive visibility into system operations.