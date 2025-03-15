package service

import (
	"context"
	"fmt"
	"project/internal/config"

	"github.com/mattermost/mattermost-server/v6/model"
)

type MattermostService struct {
	name     string
	workflow string
	running  bool

	client   *model.Client4
	wsClient *model.WebSocketClient
	config   config.MattermostConfig
	msgChan  chan *model.WebSocketEvent
}

// NewMattermostService creates a new Mattermost service
func NewMattermostService(name, workflow string, config config.MattermostConfig) *MattermostService {
	return &MattermostService{
		name:     name,
		workflow: workflow,
		config:   config,
		msgChan:  make(chan *model.WebSocketEvent, 100),
		running:  false,
	}
}

// Start implements Service interface
func (s *MattermostService) Start(ctx context.Context) error {
	if s.running {
		return nil
	}

	// Initialize client
	s.client = model.NewAPIv4Client(s.config.ServerURL)

	// Login either by token or username/password
	if s.config.APIToken != "" {
		s.client.SetToken(s.config.APIToken)
	} else {
		_, _, resp := s.client.Login(s.config.Username, s.config.Password)
		if resp.Error != nil {
			return fmt.Errorf("mattermost login error: %s", resp.Error())
		}
	}

	// Setup websocket connection
	s.wsClient, _ = model.NewWebSocketClient(s.config.WebsocketURL, s.client.AuthToken)

	// Start listening for websocket events
	s.wsClient.Listen()

	s.running = true
	return nil
}

// Stop implements Service interface
func (s *MattermostService) Stop() error {
	if !s.running {
		return nil
	}

	if s.wsClient != nil {
		s.wsClient.Close()
	}

	s.running = false
	return nil
}

// Restart implements Service interface
func (s *MattermostService) Restart(ctx context.Context) error {
	err := s.Stop()
	if err != nil {
		return err
	}
	return s.Start(ctx)
}

// GetName implements Service interface
func (s *MattermostService) GetName() string {
	return s.name
}

// GetWorkflow implements Service interface
func (s *MattermostService) GetWorkflow() string {
	return s.workflow
}

// GetType implements Service interface
func (s *MattermostService) GetType() string {
	return "Mattermost"
}

// IsRunning implements Service interface
func (s *MattermostService) IsRunning() bool {
	return s.running
}

// GetMetrics implements Service interface
func (s *MattermostService) GetMetrics() map[string]interface{} {
	return map[string]interface{}{
		"running": s.running,
	}
}

// Configure implements Service interface
func (s *MattermostService) Configure(cfg interface{}) error {
	if mattermostConfig, ok := cfg.(config.MattermostConfig); ok {
		s.config = mattermostConfig
		return nil
	}
	return nil
}
