package resilience

type RateLimiter interface {
	Allow() bool
	SetRate(rate float64)
}
