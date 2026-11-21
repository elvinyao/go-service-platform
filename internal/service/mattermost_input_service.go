package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	appctx "github.com/elvinyao/go-service-platform/pkg/context"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/logger"
	"github.com/elvinyao/go-service-platform/pkg/mattermost"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

// MattermostInputServiceName is the registered name of the Mattermost bot input.
const MattermostInputServiceName = "MattermostInputService"

// MattermostInputConfig configures the Mattermost bot input.
type MattermostInputConfig struct {
	ServerURL string
	Token     string
	Listener  mattermost.ListenerConfig
}

// MattermostInputService runs a mattermost.Listener as a runtime service and
// shares its Client with the Mattermost executors.
type MattermostInputService struct {
	*BaseService
	client   *mattermost.Client
	listener *mattermost.Listener

	mu      sync.Mutex
	handler func(ctx context.Context, message ruleengine.Message)
}

// NewMattermostInputService validates cfg and builds the client and listener.
func NewMattermostInputService(name, workflow string, cfg MattermostInputConfig, version string) (*MattermostInputService, error) {
	client, err := mattermost.NewClient(cfg.ServerURL, cfg.Token, nil)
	if err != nil {
		return nil, err
	}
	s := &MattermostInputService{
		BaseService: NewBaseService(name, workflow, "mattermost-input", version),
		client:      client,
	}
	listener, err := mattermost.NewListener(client, cfg.Listener, s.dispatch)
	if err != nil {
		return nil, err
	}
	s.listener = listener
	s.AddHealthChecker(&mattermostInputChecker{service: s})
	return s, nil
}

// Client returns the bot's REST client.
func (s *MattermostInputService) Client() *mattermost.Client {
	return s.client
}

// OnMessage sets the function that receives each routed event.
func (s *MattermostInputService) OnMessage(handler func(ctx context.Context, message ruleengine.Message)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handler = handler
}

func (s *MattermostInputService) dispatch(ctx context.Context, message ruleengine.Message) {
	s.mu.Lock()
	handler := s.handler
	s.mu.Unlock()
	if handler == nil {
		logger.WarnfWithContext(ctx, "No message handler registered for service %s; dropping %s %s", s.GetName(), message.Type, message.ID)
		return
	}
	handler(ctx, message)
}

// Start checks the bot token and starts the event stream. A rejected token
// fails startup; an unreachable server is retried in the background and
// reported by the health check.
func (s *MattermostInputService) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = appctx.WithOperationName(ctx, "start_service")
	if s.IsRunning(ctx) {
		return nil
	}
	s.TransitionState(ServiceStateStarting)
	if err := s.listener.Start(ctx); err != nil {
		s.TransitionState(ServiceStateStopped)
		return err
	}
	s.TransitionState(ServiceStateRunning)
	logger.InfofWithContext(ctx, "Service %s started for %s", s.GetName(), s.client.ServerURL())
	return nil
}

// Stop closes the event stream.
func (s *MattermostInputService) Stop(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = appctx.WithOperationName(ctx, "stop_service")
	if !s.IsRunning(ctx) {
		return nil
	}
	s.TransitionState(ServiceStateStopping)
	err := s.listener.Stop(ctx)
	s.TransitionState(ServiceStateStopped)
	if err != nil {
		return err
	}
	logger.InfofWithContext(ctx, "Service %s stopped successfully", s.GetName())
	return nil
}

// Restart stops and starts the event stream.
func (s *MattermostInputService) Restart(ctx context.Context) error {
	if err := s.Stop(ctx); err != nil {
		return err
	}
	return s.Start(ctx)
}

// GetMetrics adds listener counters to the base metrics. The token is never
// included.
func (s *MattermostInputService) GetMetrics(ctx context.Context) map[string]interface{} {
	metrics := s.BaseService.GetMetrics(ctx)
	stats := s.listener.Stats()
	metrics["server_url"] = s.client.ServerURL()
	metrics["connected"] = stats.Connected
	metrics["bot_username"] = stats.BotUsername
	metrics["connects"] = stats.Connects
	metrics["routed_events"] = stats.Routed
	metrics["ignored_events"] = stats.Ignored
	metrics["malformed_events"] = stats.Malformed
	if !stats.LastEventAt.IsZero() {
		metrics["last_event_at"] = stats.LastEventAt.Format(time.RFC3339)
	}
	return metrics
}

// mattermostInputChecker is critical: a bot that cannot read events is not
// ready, even while it keeps reconnecting.
type mattermostInputChecker struct {
	service *MattermostInputService
}

func (c *mattermostInputChecker) Check(ctx context.Context) *health.CheckResult {
	result := health.NewCheckResult("mattermost-input", health.CategoryConnectivity)
	result.Level = health.LevelCritical
	stats := c.service.listener.Stats()
	result.AddDetail("server_url", c.service.client.ServerURL())
	result.AddDetail("bot_username", stats.BotUsername)
	result.AddDetail("connects", stats.Connects)

	switch {
	case !c.service.IsRunning(ctx):
		result.SetStatus(health.StatusDown, "Mattermost input is not running")
	case !stats.Connected:
		result.SetStatus(health.StatusDown, "Mattermost event stream is not connected")
	default:
		result.SetStatus(health.StatusUp, fmt.Sprintf("Connected to Mattermost as @%s", stats.BotUsername))
	}
	result.Complete()
	return result
}
