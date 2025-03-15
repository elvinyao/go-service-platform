# Go Service Workflow

一个基于Go语言开发的微服务工作流系统，用于处理和协调不同服务之间的消息传递和业务流程。

## 系统架构

### 目录结构
```
.
├── cmd/            # 主程序入口
├── internal/       # 内部包
│   ├── config/      # 配置定义
│   ├── connections/ # 连接管理
│   ├── dataaccess/  # 数据访问层
│   ├── interfaces/  # 接口定义
│   ├── manager/     # 管理器组件
│   ├── messaging/   # 消息处理
│   ├── model/       # 数据模型
│   ├── service/     # 服务实现
│   └── workflow/    # 工作流定义
├── pkg/            # 公共包
│   └── logger/      # 日志组件
├── config/         # 配置文件
├── bin/           # 编译输出目录
└── .vscode/       # VSCode配置
```

### 系统架构图

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

### 组件关系

1. **WebSocket Service** 
   - 接收外部消息并转发到工作流管理器
   - 通过回调机制与工作流管理器集成

2. **Mattermost Service**
   - 与Mattermost系统集成
   - 提供实时消息处理功能
   - 通过Websocket与Mattermost服务器通信

3. **Workflow Manager**
   - 注册和管理多个工作流
   - 将消息分发到合适的工作流处理器
   - 通过服务管理器访问其他服务

4. **Service Manager**
   - 集中管理所有服务的生命周期
   - 提供服务发现功能
   - 监控服务健康状态并自动重启

5. **Cache Data Accessor**
   - 为Confluence服务提供缓存支持
   - 基于BigCache实现高性能缓存
   - 通过接口隔离缓存实现细节

6. **BadgeDB Service**
   - 全局服务，被所有工作流共享
   - 提供处理方法查询功能
   - 与工作流之间通过服务管理器通信

### 核心组件

1. **服务管理器 (ServiceManager)**
   - 负责服务的注册、启动、停止和监控
   - 提供服务发现和获取功能
   - 支持服务健康检查和自动重启

2. **工作流管理器 (WorkflowManager)**
   - 管理不同的工作流程
   - 处理消息的分发和路由
   - 协调不同服务之间的交互

3. **数据访问层 (DataAccessor)**
   - 提供统一的数据访问接口
   - 实现了基于BigCache的缓存机制
   - 支持数据的存取操作

4. **日志系统**
   - 基于logrus实现
   - 支持不同级别的日志记录
   - 可配置的输出格式

### 服务类型

1. **Confluence服务**
   - 负责与Confluence系统交互
   - 支持数据获取和处理
   - 可配置工作流关联

2. **WebSocket服务**
   - 处理实时消息通信
   - 支持消息订阅和推送
   - 集成消息处理回调

3. **BadgeDB服务**
   - 处理徽章相关的数据库操作
   - 提供处理方法的查询
   - 全局服务支持

4. **Mattermost服务**
   - 提供与Mattermost平台的集成
   - 支持通过API和WebSocket与Mattermost通信
   - 处理频道消息和用户事件

## 业务架构

### 业务架构图

```
+-------------------+     +-----------------+     +-------------------+
|                   |     |                 |     |                   |
|  外部客户端        +---->+  WebSocket服务  +---->+  Mattermost服务   |
|                   |     |                 |     |                   |
+-------------------+     +--------+--------+     +-------------------+
                                   |
                                   | 消息
                                   v
+-------------------+     +--------+--------+     +-------------------+
|                   |     |                 |     |                   |
|  Confluence系统   <-----+    工作流A       +---->+     BadgeDB数据库  |
|                   |     |                 |     |                   |
+-------------------+     +-----------------+     +-------------------+
        ^
        |
        |
+-------+----------+
|                  |
|  缓存数据访问层    |
|                  |
+------------------+
```

### 消息流转
1. WebSocket或Mattermost服务接收外部消息
2. 消息被转发到工作流管理器
3. 工作流管理器根据消息类型选择合适的工作流
4. 工作流处理消息并协调相关服务

### 工作流处理流程
1. **消息接收**
   - WebSocket/Mattermost服务接收外部消息
   - 消息通过回调函数传递给工作流管理器
   - 工作流管理器将消息分发给注册的工作流

2. **消息处理 (WorkflowA示例)**
   - 获取BadgeDB服务，查询消息类型对应的处理方法
   - 根据处理方法查找对应的Confluence服务
   - 调用Confluence服务获取数据
   - 记录处理结果到日志

3. **数据缓存**
   - Confluence服务使用缓存数据访问层存储数据
   - 缓存基于BigCache实现，支持高性能读写
   - 缓存数据有过期时间，自动清理过期数据

### 工作流处理
1. **WorkflowA**
   - 接收消息并解析类型
   - 查询BadgeDB获取处理方式
   - 调用对应的Confluence服务处理数据
   - 记录处理结果

### 配置管理
- 配置结构分为多个特定服务配置对象
- 每个服务的配置通过接口进行类型安全的传递
- 支持动态配置更新（通过Configure方法）

## 技术栈

- Go 1.22.5
- 依赖管理：Go Modules
- 缓存：BigCache v3.1.0
- 日志：Logrus v1.9.3
- Mattermost API：mattermost-server/v6 包

## 服务接口规范

所有服务都实现了统一的Service接口，包括以下方法：

```go
type Service interface {
	Start(ctx context.Context) error
	Stop() error
	Restart(ctx context.Context) error
	GetName() string
	GetWorkflow() string
	GetType() string
	IsRunning() bool
	GetMetrics() map[string]interface{} // 提供服务指标
	Configure(config interface{}) error // 支持动态配置
}
```

## 开发环境

### 要求
- Go 1.22.5 或更高版本
- VSCode（可选，已配置调试设置）

### 调试
- 使用VSCode的Go插件
- 已配置launch.json用于调试
- 调试输出位于bin目录

## 启动方式

```bash
# 编译
go build -o bin/service-workflow cmd/main.go

# 运行
./bin/service-workflow
```

## 扩展性

1. **新增服务**
   - 实现Service接口（包括新增的GetMetrics和Configure方法）
   - 在main.go中的registerAndStartServices函数中注册
   - 服务需实现所有必要的接口方法以确保兼容性

2. **新增工作流**
   - 实现Workflow接口
   - 在main.go中的initWorkflowManager函数中注册
   - 设计合适的消息处理逻辑

3. **数据访问扩展**
   - 实现DataAccessor接口
   - 注入到需要的服务中

这个系统设计良好，具有高度的可扩展性和模块化特性。通过接口定义和依赖注入，使得各个组件之间松耦合，易于测试和维护。系统的监控和错误处理机制也比较完善，能够保证服务的稳定运行。服务接口的扩展支持了更丰富的功能，包括指标收集和动态配置。

## New Features

### Structured Logging

The platform now includes improved structured logging with the following features:

- **JSON Formatted Logs**: All logs are now in JSON format by default for better parsing and indexing in log management systems
- **Context-aware Logging**: Logs include request IDs, trace IDs, and other contextual information
- **Configurable Log Levels**: Easily configure log levels via environment variables
- **File Rotation**: Support for log file rotation based on size and time
- **Multiple Outputs**: Logs can be sent to multiple destinations simultaneously
- **Performance Metrics**: Operations automatically include duration metrics
- **Error Details**: Enhanced error logging with structured error details

Environment variables for configuring logging:

- `LOG_LEVEL`: Set the log level (debug, info, warn, error, fatal)
- `LOG_FORMAT`: Log format (json or text)
- `LOG_TIME_FORMAT`: Timestamp format
- `LOG_CALLER_INFO`: Include caller information (true/false)
- `LOG_OUTPUT`: Output destination (stdout, stderr, or file path)

Example usage:

```go
// Initialize logger with environment variables
logger.InitFromEnv()

// Log with structured fields
logger.WithFields(logrus.Fields{
    "version": appVersion,
    "pid":     os.Getpid(),
}).Info("Application starting")

// Context-aware logging
logger.InfoWithContext(ctx, "Operation completed")

// Log operations with timing metrics
err := logger.LogOperation(ctx, "database_query", func(ctx context.Context) error {
    // Operation code here
    return nil
})
```

### Graceful Shutdown

The platform now includes a robust graceful shutdown mechanism:

- **Signal Handling**: Properly handles SIGINT, SIGTERM, and SIGQUIT signals
- **Shutdown Sequence**: Implements a coordinated shutdown sequence to ensure resources are released properly
- **Timeout Handling**: Enforces timeouts during shutdown to prevent hanging
- **Resource Cleanup**: Ensures all resources (servers, connections, goroutines) are properly cleaned up
- **Contextual Shutdown**: Uses contexts to propagate shutdown signals throughout the application

The shutdown sequence follows this order:

1. Stop monitoring services first to avoid log spam during shutdown
2. Shutdown HTTP servers
3. Stop all application services
4. Release resources

Example configuration:

```go
// Set shutdown timeouts
const (
    shutdownTimeout = 30 * time.Second        // Overall shutdown timeout
    serviceShutdownTimeout = 10 * time.Second // Per-service shutdown timeout
)
```

## Using This Platform
