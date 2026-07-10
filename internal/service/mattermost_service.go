package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/elvinyao/go-service-platform/internal/config"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/logger"

	"github.com/gorilla/websocket"
)

const mattermostRequestTimeout = 10 * time.Second

type mattermostPost struct {
	ChannelID string `json:"channel_id"`
	Message   string `json:"message"`
}

type mattermostEvent struct {
	Event string `json:"event"`
}

type MattermostService struct {
	*BaseService
	mu         sync.RWMutex
	httpClient *http.Client
	baseClient *http.Client
	wsClient   *websocket.Conn
	config     config.MattermostConfig
	authToken  string
	wsCancel   context.CancelFunc
	wsDone     chan struct{}
	generation uint64
}

// NewMattermostService creates a Mattermost-like demo service.
func NewMattermostService(name, workflow string, config config.MattermostConfig, version string, clients ...*http.Client) *MattermostService {
	var baseClient *http.Client
	if len(clients) > 0 {
		baseClient = clients[0]
	}
	s := &MattermostService{
		BaseService: NewBaseService(name, workflow, "mattermost", version),
		config:      config,
		baseClient:  baseClient,
	}
	s.AddHealthChecker(&mattermostConnectionChecker{service: s})
	return s
}

// Start initializes the bounded HTTP client and optional WebSocket connection.
func (s *MattermostService) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	if s.IsRunning(ctx) {
		s.mu.Unlock()
		return nil
	}

	httpClient := s.baseClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: mattermostRequestTimeout}
	}
	authToken := s.config.APIToken
	if authToken == "" && s.config.Username != "" {
		token, err := loginMattermost(ctx, httpClient, s.config.ServerURL, s.config.Username, s.config.Password)
		if err != nil {
			logger.WarnfWithContext(ctx, "Mattermost login failed: %v", err)
		} else {
			authToken = token
		}
	}

	s.httpClient = httpClient
	s.authToken = authToken
	s.generation++
	generation := s.generation
	wsCtx, wsCancel := context.WithCancel(ctx)
	s.wsCancel = wsCancel
	s.wsDone = make(chan struct{})
	done := s.wsDone
	websocketURL := s.config.WebsocketURL
	s.LockRunning(true)
	s.mu.Unlock()

	go s.connectWebSocket(wsCtx, generation, websocketURL, authToken, done)
	return nil
}

func loginMattermost(ctx context.Context, client *http.Client, serverURL, username, password string) (string, error) {
	payload, err := json.Marshal(map[string]string{
		"login_id": username,
		"password": password,
	})
	if err != nil {
		return "", fmt.Errorf("marshal Mattermost login: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(serverURL, "/")+"/api/v4/users/login", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create Mattermost login request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("execute Mattermost login: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return "", fmt.Errorf("Mattermost login returned status %d: %s", response.StatusCode, string(body))
	}

	token := response.Header.Get("Token")
	if token == "" {
		return "", fmt.Errorf("Mattermost login response did not include a token")
	}
	return token, nil
}

func (s *MattermostService) connectWebSocket(ctx context.Context, generation uint64, websocketURL, authToken string, done chan<- struct{}) {
	defer close(done)
	if websocketURL == "" {
		return
	}

	dialer := *websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second
	headers := http.Header{}
	if authToken != "" {
		headers.Set("Authorization", "Bearer "+authToken)
	}
	connection, _, err := dialer.DialContext(ctx, mattermostWebSocketURL(websocketURL), headers)
	if err != nil {
		if ctx.Err() == nil {
			logger.WarnfWithContext(ctx, "Failed to create Mattermost WebSocket client: %v (HTTP API still available)", err)
		}
		return
	}

	s.mu.Lock()
	if s.generation != generation || !s.IsRunning(ctx) {
		s.mu.Unlock()
		connection.Close()
		return
	}
	s.wsClient = connection
	s.mu.Unlock()

	defer func() {
		connection.Close()
		s.mu.Lock()
		if s.generation == generation && s.wsClient == connection {
			s.wsClient = nil
		}
		s.mu.Unlock()
	}()

	if authToken != "" {
		_ = connection.WriteJSON(map[string]interface{}{
			"seq":    1,
			"action": "authentication_challenge",
			"data":   map[string]string{"token": authToken},
		})
	}

	for {
		var event mattermostEvent
		if err := connection.ReadJSON(&event); err != nil {
			if ctx.Err() == nil {
				logger.DebugfWithContext(ctx, "Mattermost WebSocket closed: %v", err)
			}
			return
		}
		logger.InfofWithContext(ctx, "Received Mattermost event: %s", event.Event)
	}
}

func mattermostWebSocketURL(baseURL string) string {
	baseURL = strings.TrimRight(baseURL, "/")
	if strings.HasSuffix(baseURL, "/api/v4/websocket") {
		return baseURL
	}
	return baseURL + "/api/v4/websocket"
}

// Stop cancels connection setup, closes the active socket, and waits for cleanup.
func (s *MattermostService) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if !s.IsRunning(ctx) {
		s.mu.Unlock()
		return nil
	}

	s.generation++
	connection := s.wsClient
	cancel := s.wsCancel
	done := s.wsDone
	s.wsClient = nil
	s.wsCancel = nil
	s.httpClient = nil
	s.LockRunning(false)
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	if connection != nil {
		connection.Close()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Restart stops and starts the service with its current configuration.
func (s *MattermostService) Restart(ctx context.Context) error {
	if err := s.Stop(ctx); err != nil {
		return err
	}
	return s.Start(ctx)
}

// SendMessage sends a message to the configured default channel.
func (s *MattermostService) SendMessage(ctx context.Context, message string) error {
	s.mu.RLock()
	channel := s.config.Channel
	s.mu.RUnlock()
	return s.SendMessageToChannel(ctx, channel, message)
}

// SendMessageToChannel posts a message to a Mattermost-like HTTP endpoint.
func (s *MattermostService) SendMessageToChannel(ctx context.Context, channelID, message string) error {
	if !s.IsRunning(ctx) {
		return fmt.Errorf("mattermost service is not running")
	}
	if channelID == "" {
		return fmt.Errorf("channel ID is required")
	}

	s.mu.RLock()
	client := s.httpClient
	serverURL := s.config.ServerURL
	authToken := s.authToken
	s.mu.RUnlock()
	if client == nil {
		return fmt.Errorf("mattermost client is not initialized")
	}

	payload, err := json.Marshal(mattermostPost{ChannelID: channelID, Message: message})
	if err != nil {
		return fmt.Errorf("marshal Mattermost post: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(serverURL, "/")+"/api/v4/posts", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create Mattermost post request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		request.Header.Set("Authorization", "Bearer "+authToken)
	}

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("send Mattermost post: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
		return fmt.Errorf("Mattermost post returned status %d: %s", response.StatusCode, string(body))
	}
	return nil
}

// GetChannelID returns the configured default channel ID.
func (s *MattermostService) GetChannelID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.Channel
}

// GetMetrics returns non-sensitive Mattermost demo metrics.
func (s *MattermostService) GetMetrics(ctx context.Context) map[string]interface{} {
	baseMetrics := s.BaseService.GetMetrics(ctx)

	s.mu.RLock()
	defer s.mu.RUnlock()
	metrics := make(map[string]interface{}, len(baseMetrics)+4)
	for key, value := range baseMetrics {
		metrics[key] = value
	}
	metrics["server_url"] = s.config.ServerURL
	metrics["websocket_url"] = s.config.WebsocketURL
	metrics["channel"] = s.config.Channel
	metrics["api_token_configured"] = s.config.APIToken != "" || s.authToken != ""
	return metrics
}

// Configure replaces the service configuration for the next restart.
func (s *MattermostService) Configure(ctx context.Context, cfg interface{}) error {
	newConfig, ok := cfg.(config.MattermostConfig)
	if !ok {
		return fmt.Errorf("invalid configuration type for MattermostService")
	}
	if err := validateMattermostServiceConfig(newConfig); err != nil {
		return err
	}

	s.mu.Lock()
	s.config = newConfig
	s.mu.Unlock()
	return nil
}

func validateMattermostServiceConfig(cfg config.MattermostConfig) error {
	if err := validateAbsoluteServiceURL(cfg.ServerURL, "http", "https"); err != nil {
		return fmt.Errorf("invalid Mattermost server URL: %w", err)
	}
	if cfg.Channel == "" {
		return fmt.Errorf("Mattermost channel is required")
	}
	if cfg.WebsocketURL != "" {
		if err := validateAbsoluteServiceURL(cfg.WebsocketURL, "ws", "wss"); err != nil {
			return fmt.Errorf("invalid Mattermost WebSocket URL: %w", err)
		}
	}
	return nil
}

func validateAbsoluteServiceURL(rawURL string, schemes ...string) error {
	parsed, err := url.ParseRequestURI(rawURL)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("URL must be absolute")
	}
	for _, scheme := range schemes {
		if parsed.Scheme == scheme {
			return nil
		}
	}
	return fmt.Errorf("URL scheme must be one of %s", strings.Join(schemes, ", "))
}

type mattermostConnectionChecker struct {
	service *MattermostService
}

func (c *mattermostConnectionChecker) Check(ctx context.Context) *health.CheckResult {
	result := health.NewCheckResult("mattermost-connection", health.CategoryConnectivity)
	result.Level = health.LevelWarning

	c.service.mu.RLock()
	client := c.service.httpClient
	wsClient := c.service.wsClient
	serverURL := c.service.config.ServerURL
	websocketURL := c.service.config.WebsocketURL
	c.service.mu.RUnlock()

	if client == nil {
		result.SetStatus(health.StatusDegraded, "Mattermost HTTP client is not initialized")
	} else if wsClient == nil {
		result.SetStatus(health.StatusDegraded, "Mattermost WebSocket is unavailable; HTTP actions remain available")
	} else {
		result.SetStatus(health.StatusUp, "Mattermost WebSocket connection established")
	}
	result.AddDetail("server_url", serverURL)
	result.AddDetail("websocket_url", websocketURL)
	result.Complete()
	return result
}
