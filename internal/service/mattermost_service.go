package service

import (
	"context"
	"fmt"
	"project/internal/config"
	"project/pkg/health"

	"github.com/mattermost/mattermost-server/v6/model"
)

type MattermostService struct {
	*BaseService
	client   *model.Client4
	wsClient *model.WebSocketClient
	config   config.MattermostConfig
	msgChan  chan *model.WebSocketEvent
}

// NewMattermostService creates a new Mattermost service
func NewMattermostService(name, workflow string, config config.MattermostConfig, version string) *MattermostService {
	return &MattermostService{
		BaseService: NewBaseService(name, workflow, "mattermost", version),
		config:      config,
		msgChan:     make(chan *model.WebSocketEvent, 100),
	}
}

// Start implements Service interface
func (s *MattermostService) Start(ctx context.Context) error {
	if s.IsRunning(ctx) {
		return nil
	}

	// Initialize client
	s.client = model.NewAPIv4Client(s.config.ServerURL)

	// Login either by token or username/password
	if s.config.APIToken != "" {
		s.client.SetToken(s.config.APIToken)
	} else if s.config.Username != "" {
		_, _, err := s.client.Login(s.config.Username, s.config.Password)
		if err != nil {
			// Log warning but don't fail - service can still work for sending
			fmt.Printf("Warning: Mattermost login failed: %v\n", err)
		}
	}

	// Try to initialize WebSocket client (non-blocking)
	// If it fails, we still mark service as running for HTTP API usage
	go func() {
		wsClient, err := model.NewWebSocketClient4(s.config.WebsocketURL, s.client.AuthToken)
		if err != nil {
			fmt.Printf("Warning: Failed to create WebSocket client: %v (HTTP API still available)\n", err)
			return
		}

		s.wsClient = wsClient
		s.wsClient.Listen()
		s.wsClient.EventChannel = s.msgChan

		// Start message processing goroutine
		go s.processMessages(ctx)
	}()

	s.LockRunning(true)
	return nil
}

// Stop implements Service interface
func (s *MattermostService) Stop(ctx context.Context) error {
	if !s.IsRunning(ctx) {
		return nil
	}

	// Close WebSocket connection
	if s.wsClient != nil {
		s.wsClient.Close()
		s.wsClient = nil
	}

	// Clear client
	s.client = nil

	s.LockRunning(false)
	return nil
}

// Restart implements Service interface
func (s *MattermostService) Restart(ctx context.Context) error {
	if err := s.Stop(ctx); err != nil {
		return err
	}
	return s.Start(ctx)
}

// processMessages handles incoming WebSocket messages
func (s *MattermostService) processMessages(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-s.msgChan:
			if !ok {
				return
			}
			// Process the event
			fmt.Printf("Received event: %v\n", event.EventType())
		}
	}
}

// SendMessage sends a message to the default configured channel
func (s *MattermostService) SendMessage(ctx context.Context, message string) error {
	return s.SendMessageToChannel(ctx, s.config.Channel, message)
}

// SendMessageToChannel sends a message to a specific channel
func (s *MattermostService) SendMessageToChannel(ctx context.Context, channelID, message string) error {
	if !s.IsRunning(ctx) {
		return fmt.Errorf("mattermost service is not running")
	}

	if s.client == nil {
		return fmt.Errorf("mattermost client is not initialized")
	}

	if channelID == "" {
		return fmt.Errorf("channel ID is required")
	}

	post := &model.Post{
		ChannelId: channelID,
		Message:   message,
	}

	_, _, err := s.client.CreatePost(post)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// GetChannelID returns the default configured channel ID
func (s *MattermostService) GetChannelID() string {
	return s.config.Channel
}

// Configure implements Service interface
func (s *MattermostService) Configure(ctx context.Context, cfg interface{}) error {
	newConfig, ok := cfg.(config.MattermostConfig)
	if !ok {
		return fmt.Errorf("invalid configuration type for MattermostService")
	}

	s.config = newConfig
	return nil
}

// ReportHealth implements the Reporter interface
func (s *MattermostService) ReportHealth(ctx context.Context, report *health.Report) {
	// Use the base service's implementation
	s.BaseService.ReportHealth(ctx, report)

	// Add Mattermost-specific health checks
	result := health.NewCheckResult("mattermost-connection", health.CategoryConnectivity)
	result.Level = health.LevelCritical

	if s.client == nil || s.wsClient == nil {
		result.SetStatus(health.StatusDown, "Mattermost client not initialized")
	} else {
		// The demo adapter treats initialized clients as connected.
		result.SetStatus(health.StatusUp, "Mattermost WebSocket connection established")

		// Add connection details
		result.AddDetail("server_url", s.config.ServerURL)
		result.AddDetail("websocket_url", s.config.WebsocketURL)
	}

	result.Complete()

	// Add result to report
	checkResult := *result
	report.CheckResults = append(report.CheckResults, checkResult)
}
