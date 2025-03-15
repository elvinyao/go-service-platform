package service

import "context"

type Service interface {
	Start(ctx context.Context) error
	Stop() error
	Restart(ctx context.Context) error
	GetName() string
	GetWorkflow() string
	GetType() string
	IsRunning() bool
	GetMetrics() map[string]interface{} // 新增：获取服务指标
	Configure(config interface{}) error // 新增：动态配置服务
}
