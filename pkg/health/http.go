package health

import (
	"context"
	"encoding/json"
	appctx "github.com/elvinyao/go-service-platform/pkg/context"
	"github.com/elvinyao/go-service-platform/pkg/logger"
	"net/http"
	"time"
)

// HealthHandler provides HTTP handlers for health check endpoints
type HealthHandler struct {
	manager *HealthManager
}

// NewHealthHandler creates a new health handler
func NewHealthHandler(manager *HealthManager) *HealthHandler {
	return &HealthHandler{
		manager: manager,
	}
}

// HandleSystemHealth handles system-wide health check requests
func (h *HealthHandler) HandleSystemHealth(w http.ResponseWriter, r *http.Request) {
	ctx := appctx.FromRequest(r)
	ctx = appctx.WithOperationName(ctx, "system_health")

	// Get system health
	report := h.manager.GetHealthReport(ctx)

	// Set response status code based on health status
	statusCode := http.StatusOK
	switch report.Status {
	case StatusDown:
		statusCode = http.StatusServiceUnavailable
	case StatusUnknown:
		statusCode = http.StatusServiceUnavailable
	}

	// Set content type
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	// Write response
	if err := json.NewEncoder(w).Encode(report); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to encode health check response")
	}
}

// HandleServiceHealth handles service-specific health check requests
func (h *HealthHandler) HandleServiceHealth(w http.ResponseWriter, r *http.Request) {
	ctx := appctx.FromRequest(r)
	ctx = appctx.WithOperationName(ctx, "service_health")

	// Get service name from query parameter
	serviceName := r.URL.Query().Get("service")
	if serviceName == "" {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "Missing required query parameter 'service'"}`))
		return
	}

	// Get service health
	serviceInstance, exists := h.getServiceByName(ctx, serviceName)
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error": "Service not found"}`))
		return
	}

	// Create service health report
	report := &Report{
		ServiceName: serviceName,
		StartTime:   h.manager.startTime,
		Version:     h.manager.version,
		RefreshedAt: time.Now(),
	}

	// Add service check result
	result := NewCheckResult("service."+serviceName, CategoryDependency)
	result.Level = LevelCritical

	if serviceInstance.IsRunning(ctx) {
		result.SetStatus(StatusUp, "Service is running")

		// Add metrics
		metrics := serviceInstance.GetMetrics(ctx)
		for k, v := range metrics {
			result.AddDetail(k, v)
		}
	} else {
		result.SetStatus(StatusDown, "Service is not running")
	}

	result.Complete()

	// Add result to report
	checkResult := *result
	report.CheckResults = append(report.CheckResults, checkResult)
	report.Status = result.Status

	// Set response status code based on health status
	statusCode := http.StatusOK
	if report.Status != StatusUp {
		statusCode = http.StatusServiceUnavailable
	}

	// Set content type
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	// Write response
	if err := json.NewEncoder(w).Encode(report); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to encode health check response")
	}
}

// HandleReadinessCheck handles readiness check requests
func (h *HealthHandler) HandleReadinessCheck(w http.ResponseWriter, r *http.Request) {
	// Immediately fail readiness during shutdown so K8s stops routing traffic
	if h.manager.IsShuttingDown() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status": "NOT_READY", "reason": "shutting_down"}`))
		return
	}

	ctx := appctx.FromRequest(r)
	ctx = appctx.WithOperationName(ctx, "readiness_check")

	// Get system health
	report := h.manager.GetHealthReport(ctx)

	// Warning-level degradation remains ready; critical failures produce DOWN.
	if report.Status == StatusDown || report.Status == StatusUnknown {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte(`{"status": "NOT_READY"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "READY"}`))
}

// HandleLivenessCheck handles liveness check requests
func (h *HealthHandler) HandleLivenessCheck(w http.ResponseWriter, r *http.Request) {
	// Reaching this handler proves the process and admin server are alive.
	// Dependency health belongs to readiness and must not trigger restart loops.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status": "UP"}`))
}

// RegisterHTTPHandlers registers all health check handlers with the provided mux
func (h *HealthHandler) RegisterHTTPHandlers(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", h.HandleSystemHealth)
	mux.HandleFunc("GET /health/service", h.HandleServiceHealth)
	mux.HandleFunc("GET /health/readiness", h.HandleReadinessCheck)
	mux.HandleFunc("GET /health/liveness", h.HandleLivenessCheck)
}

// getServiceByName returns a service by name from the health manager
func (h *HealthHandler) getServiceByName(ctx context.Context, name string) (ServiceChecker, bool) {
	h.manager.mu.RLock()
	defer h.manager.mu.RUnlock()

	service, exists := h.manager.serviceInstances[name]
	return service, exists
}
