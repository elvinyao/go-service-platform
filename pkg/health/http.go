package health

import (
	"encoding/json"
	"net/http"
	appctx "project/pkg/context"
	"project/pkg/logger"
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
	report := h.manager.GetSystemHealth(ctx)

	// Set response status code based on health status
	statusCode := http.StatusOK
	switch report.Status {
	case StatusDegraded:
		statusCode = http.StatusServiceUnavailable
	case StatusDown:
		statusCode = http.StatusServiceUnavailable
	case StatusUnknown:
		statusCode = http.StatusInternalServerError
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

	// Get service name from URL
	serviceName := r.URL.Query().Get("service")
	if serviceName == "" {
		http.Error(w, "Service name is required", http.StatusBadRequest)
		return
	}

	ctx = appctx.WithOperationName(ctx, "service_health")
	ctx = appctx.WithServiceName(ctx, serviceName)

	// Get service health
	report, found := h.manager.GetServiceHealth(ctx, serviceName)
	if !found {
		http.Error(w, "Service not found", http.StatusNotFound)
		return
	}

	// Set response status code based on health status
	statusCode := http.StatusOK
	switch report.Status {
	case StatusDegraded:
		statusCode = http.StatusServiceUnavailable
	case StatusDown:
		statusCode = http.StatusServiceUnavailable
	case StatusUnknown:
		statusCode = http.StatusInternalServerError
	}

	// Set content type
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	// Write response
	if err := json.NewEncoder(w).Encode(report); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to encode health check response")
	}
}

// HandleReadinessCheck handles readiness probe requests
// Returns 200 if the system is ready to accept traffic
func (h *HealthHandler) HandleReadinessCheck(w http.ResponseWriter, r *http.Request) {
	ctx := appctx.FromRequest(r)
	ctx = appctx.WithOperationName(ctx, "readiness_check")

	// Get system health
	report := h.manager.GetSystemHealth(ctx)

	// System is ready if it's UP or DEGRADED (can still handle some traffic)
	if report.Status == StatusUp || report.Status == StatusDegraded {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Not Ready"))
	}
}

// HandleLivenessCheck handles liveness probe requests
// Returns 200 if the system is alive at all, even in degraded state
func (h *HealthHandler) HandleLivenessCheck(w http.ResponseWriter, r *http.Request) {
	ctx := appctx.FromRequest(r)
	ctx = appctx.WithOperationName(ctx, "liveness_check")

	// Get system health
	report := h.manager.GetSystemHealth(ctx)

	// System is alive if it's not completely DOWN
	if report.Status != StatusDown {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("Not Alive"))
	}
}

// RegisterHTTPHandlers registers all health check handlers to the provided mux
func (h *HealthHandler) RegisterHTTPHandlers(mux *http.ServeMux) {
	mux.HandleFunc("/health", h.HandleSystemHealth)
	mux.HandleFunc("/health/service", h.HandleServiceHealth)
	mux.HandleFunc("/health/ready", h.HandleReadinessCheck)
	mux.HandleFunc("/health/live", h.HandleLivenessCheck)
}
