package metrics

import "time"

type MetricsCollector interface {
	RecordCounter(name string, value int64, tags map[string]string)
	RecordGauge(name string, value float64, tags map[string]string)
	RecordLatency(name string, duration time.Duration, tags map[string]string)
}
