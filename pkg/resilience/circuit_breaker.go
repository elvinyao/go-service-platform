package resilience

type CircuitBreaker interface {
	Execute(cmd func() (interface{}, error)) (interface{}, error)
	GetState() string
}
