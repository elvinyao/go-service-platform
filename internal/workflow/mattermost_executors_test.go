package workflow

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/elvinyao/go-service-platform/internal/manager"
	"github.com/elvinyao/go-service-platform/internal/service"
	runtimeconfig "github.com/elvinyao/go-service-platform/pkg/config"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/mattermost"
)

type namedService struct{ name string }

func (s namedService) Start(context.Context) error                       { return nil }
func (s namedService) Stop(context.Context) error                        { return nil }
func (s namedService) Restart(context.Context) error                     { return nil }
func (s namedService) GetName() string                                   { return s.name }
func (s namedService) GetWorkflow() string                               { return "test" }
func (s namedService) GetType() string                                   { return "test" }
func (s namedService) IsRunning(context.Context) bool                    { return true }
func (s namedService) GetMetrics(context.Context) map[string]interface{} { return nil }
func (s namedService) RegisterHealthChecks() []health.Checker            { return nil }

func mattermostRuntime() runtimeconfig.RuntimeConfig {
	cfg := runtimeconfig.DefaultRuntimeConfig()
	cfg.Inputs.Mattermost.Enabled = true
	cfg.Inputs.Mattermost.Token = "token"
	return cfg
}

func TestWorkflowEngineRegistersMattermostExecutors(t *testing.T) {
	sm := manager.NewServiceManager("test")
	input, err := service.NewMattermostInputService(service.MattermostInputServiceName, "WorkflowEngine",
		service.MattermostInputConfig{ServerURL: "http://localhost:8065", Token: "token"}, "test")
	if err != nil {
		t.Fatalf("new input: %v", err)
	}
	if err := sm.RegisterService(input); err != nil {
		t.Fatalf("register: %v", err)
	}

	engine, err := NewWorkflowEngine(context.Background(), sm, "../../config/profiles/mattermost/rule-engine.yaml",
		"../../config/profiles/mattermost/workflow-rules.yaml", mattermostRuntime())
	if err != nil {
		t.Fatalf("new workflow engine: %v", err)
	}
	defer engine.Stop(context.Background())
	executors := engine.AdminSnapshot(context.Background()).Executors
	if !slices.Contains(executors, mattermost.PostExecutorType) || !slices.Contains(executors, mattermost.ReactionExecutorType) {
		t.Fatalf("executors = %v", executors)
	}
}

func TestWorkflowEngineNeedsTheMattermostInputService(t *testing.T) {
	cases := map[string]*manager.ServiceManager{
		"missing":    manager.NewServiceManager("test"),
		"wrong type": manager.NewServiceManager("test"),
	}
	_ = cases["wrong type"].RegisterService(namedService{name: service.MattermostInputServiceName})
	for name, sm := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := NewWorkflowEngine(context.Background(), sm, "../../config/profiles/mattermost/rule-engine.yaml",
				"../../config/profiles/mattermost/workflow-rules.yaml", mattermostRuntime())
			if err == nil || !strings.Contains(err.Error(), "Mattermost executors") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
