package service

import (
	"context"
	"encoding/json"
	"fmt"
	appctx "github.com/elvinyao/go-service-platform/pkg/context"
	"github.com/elvinyao/go-service-platform/pkg/errors"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/logger"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ConfluenceSettingsConfig holds configuration for the Confluence settings service
type ConfluenceSettingsConfig struct {
	PageID          string
	RefreshInterval time.Duration
	APIEndpoint     string
}

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

// SettingsSyncState describes the cache state of Confluence settings.
type SettingsSyncState string

const (
	SettingsSyncCold  SettingsSyncState = "cold"
	SettingsSyncFresh SettingsSyncState = "fresh"
	SettingsSyncStale SettingsSyncState = "stale"
)

type confluenceRefreshSession struct {
	generation uint64
	stop       chan struct{}
	done       chan struct{}
	cancel     context.CancelFunc
	stopOnce   sync.Once
}

func newConfluenceRefreshSession(generation uint64, cancel context.CancelFunc) *confluenceRefreshSession {
	return &confluenceRefreshSession{
		generation: generation,
		stop:       make(chan struct{}),
		done:       make(chan struct{}),
		cancel:     cancel,
	}
}

func (rs *confluenceRefreshSession) signalStop() {
	rs.stopOnce.Do(func() {
		close(rs.stop)
		if rs.cancel != nil {
			rs.cancel()
		}
	})
}

// ConfluenceSettingsService manages settings from Confluence with periodic refresh
type ConfluenceSettingsService struct {
	*BaseService
	mu                sync.RWMutex
	config            ConfluenceSettingsConfig
	settings          []SettingRule
	lastRefresh       time.Time
	refreshGeneration uint64
	refreshSession    *confluenceRefreshSession
	httpClient        *http.Client
}

// NewConfluenceSettingsService creates a new ConfluenceSettingsService
func NewConfluenceSettingsService(
	name, workflow string,
	cfg ConfluenceSettingsConfig,
	version string,
	clients ...*http.Client,
) *ConfluenceSettingsService {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	if len(clients) > 0 && clients[0] != nil {
		httpClient = clients[0]
	}
	s := &ConfluenceSettingsService{
		BaseService:    NewBaseService(name, workflow, "confluence-settings", version),
		config:         cfg,
		settings:       make([]SettingRule, 0),
		refreshSession: newConfluenceRefreshSession(0, nil),
		httpClient:     httpClient,
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
	s.TransitionState(ServiceStateStarting)

	// Initial fetch of settings
	if err := s.refreshSettingsLocked(ctx); err != nil {
		logger.WithContextError(ctx, err).Warn("Failed to fetch initial settings, will retry")
	}

	// Start generation-fenced periodic refresh
	s.refreshGeneration++
	generation := s.refreshGeneration
	refreshInterval := s.config.RefreshInterval
	refreshCtx, refreshCancel := context.WithCancel(ctx)
	session := newConfluenceRefreshSession(generation, refreshCancel)
	s.refreshSession = session
	s.TransitionState(ServiceStateRunning)
	go s.periodicRefresh(refreshCtx, generation, refreshInterval, session)

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

	s.refreshGeneration++
	session := s.refreshSession
	s.TransitionState(ServiceStateStopping)

	// Release the lock BEFORE waiting, so periodicRefresh can finish
	// its current cycle and see the stop signal
	s.mu.Unlock()

	if session != nil {
		session.signalStop()
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-session.done:
			logger.DebugfWithContext(ctx, "Periodic refresh stopped")
		case <-ctx.Done():
			s.TransitionState(ServiceStateStopped)
			return fmt.Errorf("wait for periodic refresh shutdown: %w", ctx.Err())
		case <-timer.C:
			s.TransitionState(ServiceStateStopped)
			return fmt.Errorf("timed out waiting for periodic refresh shutdown")
		}
	}

	s.TransitionState(ServiceStateStopped)
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
func (s *ConfluenceSettingsService) periodicRefresh(
	ctx context.Context,
	generation uint64,
	refreshInterval time.Duration,
	session *confluenceRefreshSession,
) {
	defer close(session.done)
	if refreshInterval <= 0 {
		return
	}

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-session.stop:
			return
		case <-ticker.C:
			if err := s.refreshSettings(ctx, generation); err != nil && ctx.Err() == nil {
				logger.WithContextError(ctx, err).Warn("Failed to refresh settings")
			}
		}
	}
}

func (s *ConfluenceSettingsService) refreshSettings(ctx context.Context, generation uint64) error {
	ctx = appctx.WithOperationName(ctx, "refresh_settings")
	logger.DebugfWithContext(ctx, "Refreshing settings from Confluence")

	settings, err := s.fetchSettingsFromConfluence(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return err
	}
	if s.refreshGeneration != generation || !s.IsRunning(ctx) {
		s.mu.Unlock()
		return nil
	}
	s.settings = settings
	s.lastRefresh = time.Now()
	s.mu.Unlock()
	logger.InfofWithContext(ctx, "Refreshed %d settings rules", len(settings))
	return nil
}

// refreshSettingsLocked fetches settings from Confluence (must hold lock)
func (s *ConfluenceSettingsService) refreshSettingsLocked(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "refresh_settings")
	logger.DebugfWithContext(ctx, "Refreshing settings from Confluence")

	settings, err := fetchConfluenceSettings(ctx, s.httpClient, s.config.APIEndpoint, s.config.PageID)
	if err != nil {
		return err
	}

	s.settings = settings
	s.lastRefresh = time.Now()

	logger.InfofWithContext(ctx, "Refreshed %d settings rules", len(settings))
	return nil
}

// fetchSettingsFromConfluence fetches settings from the Confluence REST API
func (s *ConfluenceSettingsService) fetchSettingsFromConfluence(ctx context.Context) ([]SettingRule, error) {
	s.mu.RLock()
	client := s.httpClient
	apiEndpoint := s.config.APIEndpoint
	pageID := s.config.PageID
	s.mu.RUnlock()
	return fetchConfluenceSettings(ctx, client, apiEndpoint, pageID)
}

func fetchConfluenceSettings(ctx context.Context, client *http.Client, apiEndpoint, pageID string) ([]SettingRule, error) {
	url := strings.TrimRight(apiEndpoint, "/") + "/rest/api/content/" + pageID

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, errors.New(errors.TypeServiceUnavailable, fmt.Sprintf("failed to create request: %v", err), err)
	}

	resp, err := client.Do(req)
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

	logger.InfofWithContext(ctx, "Fetched %d setting rules from Confluence page %s", len(settings.Rules), pageID)
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

// GetLastRefreshTime returns the last refresh timestamp
func (s *ConfluenceSettingsService) GetLastRefreshTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastRefresh
}

func (s *ConfluenceSettingsService) syncStateLocked(now time.Time) (SettingsSyncState, time.Duration) {
	if s.lastRefresh.IsZero() {
		return SettingsSyncCold, 0
	}
	age := now.Sub(s.lastRefresh)
	if age > s.config.RefreshInterval*2 {
		return SettingsSyncStale, age
	}
	return SettingsSyncFresh, age
}

// SyncState returns the explicit Confluence settings cache state.
func (s *ConfluenceSettingsService) SyncState() SettingsSyncState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, _ := s.syncStateLocked(time.Now())
	return state
}

// GetMetrics returns service metrics
func (s *ConfluenceSettingsService) GetMetrics(ctx context.Context) map[string]interface{} {
	baseMetrics := s.BaseService.GetMetrics(ctx)

	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	syncState, refreshAge := s.syncStateLocked(now)

	metrics := make(map[string]interface{})
	for k, v := range baseMetrics {
		metrics[k] = v
	}

	metrics["sync_state"] = string(syncState)
	metrics["settings_count"] = len(s.settings)
	metrics["refresh_interval"] = s.config.RefreshInterval.String()
	metrics["api_endpoint"] = s.config.APIEndpoint
	metrics["page_id"] = s.config.PageID

	if syncState != SettingsSyncCold {
		metrics["last_refresh"] = s.lastRefresh.Format(time.RFC3339)
		metrics["last_refresh_age_seconds"] = refreshAge.Seconds()
	}

	return metrics
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

	c.service.mu.RLock()
	syncState, refreshAge := c.service.syncStateLocked(time.Now())
	c.service.mu.RUnlock()

	result.AddDetail("sync_state", string(syncState))
	switch syncState {
	case SettingsSyncCold:
		result.SetStatus(health.StatusDegraded, "Settings have never been refreshed")
	case SettingsSyncStale:
		result.AddDetail("last_refresh_age_seconds", refreshAge.Seconds())
		result.SetStatus(health.StatusDegraded, fmt.Sprintf("Settings are stale: last refresh was %v ago", refreshAge))
	case SettingsSyncFresh:
		result.AddDetail("last_refresh_age_seconds", refreshAge.Seconds())
		result.SetStatus(health.StatusUp, fmt.Sprintf("Settings refreshed %v ago", refreshAge))
	}

	result.Complete()
	return result
}
