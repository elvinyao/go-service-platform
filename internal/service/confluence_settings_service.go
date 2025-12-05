package service

import (
	"context"
	"fmt"
	"project/internal/config"
	"project/internal/dataaccess"
	appctx "project/pkg/context"
	"project/pkg/errors"
	"project/pkg/health"
	"project/pkg/logger"
	"sync"
	"time"
)

// SettingRule represents a rule for matching events
type SettingRule struct {
	EventType   string `json:"event_type"`
	Pattern     string `json:"pattern"`
	ChannelID   string `json:"channel_id"`
	MessageTmpl string `json:"message_template"`
	Enabled     bool   `json:"enabled"`
}

// ConfluenceSettingsService manages settings from Confluence with periodic refresh
type ConfluenceSettingsService struct {
	*BaseService
	dataAccessor dataaccess.DataAccessor
	mu           sync.RWMutex
	config       config.ConfluenceSettingsConfig
	settings     []SettingRule
	lastRefresh  time.Time
	stopRefresh  chan struct{}
	refreshDone  chan struct{}
}

// NewConfluenceSettingsService creates a new ConfluenceSettingsService
func NewConfluenceSettingsService(name, workflow string, da dataaccess.DataAccessor, cfg config.ConfluenceSettingsConfig, version string) *ConfluenceSettingsService {
	s := &ConfluenceSettingsService{
		BaseService:  NewBaseService(name, workflow, "confluence-settings", version),
		dataAccessor: da,
		config:       cfg,
		settings:     make([]SettingRule, 0),
		stopRefresh:  make(chan struct{}),
		refreshDone:  make(chan struct{}),
	}

	// Add custom health checker
	s.AddHealthChecker(&settingsRefreshChecker{service: s})

	return s
}

// Start implements Service interface
func (s *ConfluenceSettingsService) Start(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "start_service")

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsRunning(ctx) {
		logger.InfofWithContext(ctx, "Service %s is already running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Starting service: %s", s.GetName())

	// Initial fetch of settings
	if err := s.refreshSettingsLocked(ctx); err != nil {
		logger.WithContextError(ctx, err).Warn("Failed to fetch initial settings, will retry")
	}

	// Start periodic refresh
	s.stopRefresh = make(chan struct{})
	s.refreshDone = make(chan struct{})
	go s.periodicRefresh(ctx)

	s.setRunningLocked(true)
	logger.InfofWithContext(ctx, "Service %s started successfully", s.GetName())
	return nil
}

// Stop implements Service interface
func (s *ConfluenceSettingsService) Stop(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "stop_service")

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning(ctx) {
		logger.InfofWithContext(ctx, "Service %s is not running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Stopping service: %s", s.GetName())

	// Stop periodic refresh
	close(s.stopRefresh)

	// Wait for refresh goroutine to complete with timeout
	select {
	case <-s.refreshDone:
		logger.DebugfWithContext(ctx, "Periodic refresh stopped")
	case <-time.After(5 * time.Second):
		logger.WarnfWithContext(ctx, "Timeout waiting for periodic refresh to stop")
	}

	s.setRunningLocked(false)
	logger.InfofWithContext(ctx, "Service %s stopped successfully", s.GetName())
	return nil
}

// Restart implements Service interface
func (s *ConfluenceSettingsService) Restart(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "restart_service")

	if err := s.Stop(ctx); err != nil {
		return err
	}
	return s.Start(ctx)
}

// periodicRefresh runs in a goroutine and periodically refreshes settings
func (s *ConfluenceSettingsService) periodicRefresh(ctx context.Context) {
	defer close(s.refreshDone)

	ticker := time.NewTicker(s.config.RefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopRefresh:
			return
		case <-ticker.C:
			s.mu.Lock()
			if err := s.refreshSettingsLocked(ctx); err != nil {
				logger.WithContextError(ctx, err).Warn("Failed to refresh settings")
			}
			s.mu.Unlock()
		}
	}
}

// refreshSettingsLocked fetches settings from Confluence (must hold lock)
func (s *ConfluenceSettingsService) refreshSettingsLocked(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "refresh_settings")
	logger.DebugfWithContext(ctx, "Refreshing settings from Confluence")

	// In a real implementation, this would call the Confluence API
	// For now, we simulate fetching settings
	settings, err := s.fetchSettingsFromConfluence(ctx)
	if err != nil {
		return err
	}

	s.settings = settings
	s.lastRefresh = time.Now()

	// Cache the settings
	if err := s.dataAccessor.SetData("confluence_settings", settings); err != nil {
		logger.WithContextError(ctx, err).Warn("Failed to cache settings")
	}

	logger.InfofWithContext(ctx, "Refreshed %d settings rules", len(settings))
	return nil
}

// fetchSettingsFromConfluence simulates fetching settings from Confluence API
func (s *ConfluenceSettingsService) fetchSettingsFromConfluence(ctx context.Context) ([]SettingRule, error) {
	// Simulate API call delay
	time.Sleep(50 * time.Millisecond)

	// In a real implementation, you would:
	// 1. Call Confluence API to get the page content
	// 2. Parse the content (JSON/structured table) to extract rules
	// 3. Return the parsed rules

	// For demonstration, return sample rules
	return []SettingRule{
		{
			EventType:   "AAA",
			Pattern:     ".*",
			ChannelID:   "default-channel",
			MessageTmpl: "Event AAA received: %s",
			Enabled:     true,
		},
		{
			EventType:   "BBB",
			Pattern:     "important.*",
			ChannelID:   "alerts-channel",
			MessageTmpl: "Important event: %s",
			Enabled:     true,
		},
	}, nil
}

// GetSettings returns all current settings
func (s *ConfluenceSettingsService) GetSettings(ctx context.Context) []SettingRule {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return a copy to avoid concurrent modification
	result := make([]SettingRule, len(s.settings))
	copy(result, s.settings)
	return result
}

// MatchEvent checks if an event matches any setting rule and returns matching rules
func (s *ConfluenceSettingsService) MatchEvent(ctx context.Context, eventType string, eventData interface{}) []SettingRule {
	ctx = appctx.WithOperationName(ctx, "match_event")
	logger.DebugfWithContext(ctx, "Matching event type: %s", eventType)

	s.mu.RLock()
	defer s.mu.RUnlock()

	var matchedRules []SettingRule

	for _, rule := range s.settings {
		if !rule.Enabled {
			continue
		}

		if rule.EventType == eventType {
			// In a real implementation, you would also check the pattern
			// against eventData using regex or other matching logic
			matchedRules = append(matchedRules, rule)
			logger.DebugfWithContext(ctx, "Matched rule for event type %s: channel=%s", eventType, rule.ChannelID)
		}
	}

	logger.InfofWithContext(ctx, "Found %d matching rules for event type %s", len(matchedRules), eventType)
	return matchedRules
}

// GetLastRefreshTime returns the last refresh timestamp
func (s *ConfluenceSettingsService) GetLastRefreshTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastRefresh
}

// GetMetrics returns service metrics
func (s *ConfluenceSettingsService) GetMetrics(ctx context.Context) map[string]interface{} {
	baseMetrics := s.BaseService.GetMetrics(ctx)

	s.mu.RLock()
	defer s.mu.RUnlock()

	metrics := make(map[string]interface{})
	for k, v := range baseMetrics {
		metrics[k] = v
	}

	metrics["settings_count"] = len(s.settings)
	metrics["refresh_interval"] = s.config.RefreshInterval.String()

	if !s.lastRefresh.IsZero() {
		metrics["last_refresh"] = s.lastRefresh.Format(time.RFC3339)
		metrics["last_refresh_age_seconds"] = time.Since(s.lastRefresh).Seconds()
	}

	return metrics
}

// Configure implements Service interface
func (s *ConfluenceSettingsService) Configure(ctx context.Context, cfg interface{}) error {
	ctx = appctx.WithOperationName(ctx, "configure_service")

	confCfg, ok := cfg.(config.ConfluenceSettingsConfig)
	if !ok {
		return errors.New(errors.TypeInvalidInput, "Invalid configuration type", nil)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.config = confCfg
	logger.InfofWithContext(ctx, "Service %s configured successfully", s.GetName())
	return nil
}

// settingsRefreshChecker checks if settings are being refreshed regularly
type settingsRefreshChecker struct {
	service *ConfluenceSettingsService
}

// Check implements the Checker interface
func (c *settingsRefreshChecker) Check(ctx context.Context) *health.CheckResult {
	result := health.NewCheckResult("settings-refresh", health.CategoryConnectivity)
	result.Level = health.LevelWarning

	if !c.service.IsRunning(ctx) {
		result.SetStatus(health.StatusDown, "Settings service is not running")
		result.Complete()
		return result
	}

	lastRefresh := c.service.GetLastRefreshTime()
	if lastRefresh.IsZero() {
		result.SetStatus(health.StatusDegraded, "Settings have never been refreshed")
		result.Complete()
		return result
	}

	refreshAge := time.Since(lastRefresh)
	result.AddDetail("last_refresh_age_seconds", refreshAge.Seconds())

	// Check if refresh is stale (more than 2x the refresh interval)
	staleThreshold := c.service.config.RefreshInterval * 2
	if refreshAge > staleThreshold {
		result.SetStatus(health.StatusDegraded, fmt.Sprintf("Settings are stale: last refresh was %v ago", refreshAge))
	} else {
		result.SetStatus(health.StatusUp, fmt.Sprintf("Settings refreshed %v ago", refreshAge))
	}

	result.Complete()
	return result
}
