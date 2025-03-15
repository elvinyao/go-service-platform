package interfaces

type HealthChecker interface {
	Check() (bool, error)
	GetStatus() string
}
