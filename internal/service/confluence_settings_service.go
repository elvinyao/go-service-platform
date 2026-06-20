package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

// confluencePage represents the Confluence REST API page response
type confluencePage struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Body  struct {
		Storage struct {
			Value string `json:"value"`
		} `json:"storage"`
	} `json:"body"`
}

// confluenceSettings represents the parsed settings from the page body
type confluenceSettings struct {
	Rules []SettingRule `json:"rules"`
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
	httpClient   *http.Client
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
		httpClient:   &http.Client{Timeout: 10 * time.Second},
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

	if !s.IsRunning(ctx) {
		s.mu.Unlock()
		logger.InfofWithContext(ctx, "Service %s is not running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Stopping service: %s", s.GetName())

	// Signal the refresh goroutine to stop and mark as not running
	close(s.stopRefresh)
	s.setRunningLocked(false)

	// Release the lock BEFORE waiting, so periodicRefresh can finish
	// its current cycle and see the stop signal
	s.mu.Unlock()

	// Wait for refresh goroutine to complete with timeout
	select {
	case <-s.refreshDone:
		logger.DebugfWithContext(ctx, "Periodic refresh stopped")
	case <-time.After(5 * time.Second):
		logger.WarnfWithContext(ctx, "Timeout waiting for periodic refresh to stop")
	}

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

// fetchSettingsFromConfluence fetches settings from the Confluence REST API
func (s *ConfluenceSettingsService) fetchSettingsFromConfluence(ctx context.Context) ([]SettingRule, error) {
	url := s.config.APIEndpoint + "/rest/api/content/" + s.config.PageID

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New(errors.TypeServiceUnavailable, fmt.Sprintf("failed to create request: %v", err), err)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, errors.New(errors.TypeServiceUnavailable, fmt.Sprintf("failed to fetch Confluence page: %v", err), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, errors.New(errors.TypeServiceUnavailable,
			fmt.Sprintf("Confluence API returned status %d: %s", resp.StatusCode, string(body)), nil)
	}

	var page confluencePage
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, errors.New(errors.TypeInvalidInput, fmt.Sprintf("failed to decode Confluence page: %v", err), err)
	}

	var settings confluenceSettings
	if err := json.Unmarshal([]byte(page.Body.Storage.Value), &settings); err != nil {
		return nil, errors.New(errors.TypeInvalidInput, fmt.Sprintf("failed to parse settings from page body: %v", err), err)
	}

	logger.InfofWithContext(ctx, "Fetched %d setting rules from Confluence page %s", len(settings.Rules), s.config.PageID)
	return settings.Rules, nil
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
			// Pattern matching is handled by the rule-engine Confluence adapter.
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
