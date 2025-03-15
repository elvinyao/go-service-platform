package interfaces

type ConfigProvider interface {
	GetConfig(key string) (interface{}, error)
	WatchConfig(key string, callback func(interface{}))
	RefreshConfig() error
}
