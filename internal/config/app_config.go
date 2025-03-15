package config

type Config struct {
	Services  map[string]interface{}
	Workflows map[string]interface{}
	System    SystemConfig
}

type SystemConfig struct {
	WorkerPoolSize int
	MetricsEnabled bool
	LogLevel       string
}
