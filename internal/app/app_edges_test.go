package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/elvinyao/go-service-platform/internal/manager"
	"github.com/elvinyao/go-service-platform/internal/model"
	"github.com/elvinyao/go-service-platform/internal/service"
	workflowimpl "github.com/elvinyao/go-service-platform/internal/workflow"
	"github.com/elvinyao/go-service-platform/pkg/config"
	"github.com/elvinyao/go-service-platform/pkg/health"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

type appTestWorkflow struct {
	name string
}

func (w appTestWorkflow) GetName() string {
	return w.name
}

func (w appTestWorkflow) ProcessMessage(ctx context.Context, msg model.Message) error {
	return nil
}

type appTestService struct {
	name string
}

func (s appTestService) Start(ctx context.Context) error    { return nil }
func (s appTestService) Stop(ctx context.Context) error     { return nil }
func (s appTestService) Restart(ctx context.Context) error  { return nil }
func (s appTestService) GetName() string                    { return s.name }
func (s appTestService) GetWorkflow() string                { return "test" }
func (s appTestService) GetType() string                    { return "test" }
func (s appTestService) IsRunning(ctx context.Context) bool { return true }
func (s appTestService) GetMetrics(ctx context.Context) map[string]interface{} {
	return map[string]interface{}{"running": true}
}
func (s appTestService) Configure(ctx context.Context, cfg interface{}) error {
	return fmt.Errorf("not configurable")
}
func (s appTestService) RegisterHealthChecks() []health.Checker { return nil }
func (s appTestService) ReportHealth(ctx context.Context, report *health.Report) {
}

type appFailingStopService struct {
	appTestService
}

func (s appFailingStopService) Stop(ctx context.Context) error {
	return fmt.Errorf("stop failed")
}

type failingResponseWriter struct {
	header http.Header
	code   int
}

func (w *failingResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *failingResponseWriter) Write([]byte) (int, error) {
	return 0, fmt.Errorf("write failed")
}

func (w *failingResponseWriter) WriteHeader(code int) {
	w.code = code
}

func TestRuleEngineInfoReturnsErrorsForMissingOrWrongWorkflow(t *testing.T) {
	wm := manager.NewWorkflowManager()

	missing := httptest.NewRecorder()
	ruleEngineInfo(missing, httptest.NewRequest(http.MethodGet, "/rule-engine", nil), wm)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing workflow status = %d, want 404", missing.Code)
	}

	if err := wm.RegisterWorkflow(appTestWorkflow{name: ruleengine.DefaultWorkflowName}); err != nil {
		t.Fatalf("register workflow: %v", err)
	}
	wrongType := httptest.NewRecorder()
	ruleEngineInfo(wrongType, httptest.NewRequest(http.MethodGet, "/rule-engine", nil), wm)
	if wrongType.Code != http.StatusInternalServerError {
		t.Fatalf("wrong workflow type status = %d, want 500", wrongType.Code)
	}
}

func TestAdminHandlersHandleEncodeErrors(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)

	sm := manager.NewServiceManager("test")
	sm.RegisterService(appTestService{name: "svc"})
	servicesList(&failingResponseWriter{}, req, sm)

	wm := manager.NewWorkflowManager()
	if err := wm.RegisterWorkflow(appTestWorkflow{name: "custom"}); err != nil {
		t.Fatalf("register custom workflow: %v", err)
	}
	workflowsList(&failingResponseWriter{}, req, wm)

	dir := t.TempDir()
	ruleConfigPath := filepath.Join(dir, "rule-engine.yaml")
	rulesPath := filepath.Join(dir, "workflow-rules.yaml")
	if err := os.WriteFile(ruleConfigPath, []byte(`
workflows:
  - name: WorkflowEngine
    providers: [yaml]
    mode: single
`), 0o644); err != nil {
		t.Fatalf("write rule config: %v", err)
	}
	if err := os.WriteFile(rulesPath, []byte(`version: "test"`), 0o644); err != nil {
		t.Fatalf("write rules: %v", err)
	}
	engine, err := workflowimpl.NewWorkflowEngine(context.Background(), manager.NewServiceManager("test"), ruleConfigPath, rulesPath)
	if err != nil {
		t.Fatalf("new workflow engine: %v", err)
	}
	if err := wm.RegisterWorkflow(engine); err != nil {
		t.Fatalf("register engine: %v", err)
	}
	ruleEngineInfo(&failingResponseWriter{}, req, wm)
}

func TestServicesListIncludesWorkflowName(t *testing.T) {
	sm := manager.NewServiceManager("test")
	sm.RegisterService(appTestService{name: "svc"})

	rec := httptest.NewRecorder()
	servicesList(rec, httptest.NewRequest(http.MethodGet, "/services", nil), sm)
	if rec.Code != http.StatusOK {
		t.Fatalf("services status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"workflow":"test"`) {
		t.Fatalf("services response missing workflow name: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"type":"test"`) {
		t.Fatalf("services response missing stable service type: %s", rec.Body.String())
	}
}

func TestAdminMuxRejectsUnsupportedMethods(t *testing.T) {
	mux := newAdminMux(
		manager.NewServiceManager("test"),
		manager.NewWorkflowManager(),
		health.NewHealthManager(time.Hour, "test"),
	)

	for _, path := range []string{
		"/health",
		"/health/service?service=missing",
		"/health/readiness",
		"/health/liveness",
		"/services",
		"/workflows",
		"/rule-engine",
	} {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("POST %s status = %d, want 405", path, recorder.Code)
		}
	}
}

func TestSetupMessageListenersReturnsMissingAndTypeMismatchErrors(t *testing.T) {
	ctx := context.Background()
	wm := manager.NewWorkflowManager()

	missing := manager.NewServiceManager("test")
	err := setupMessageListeners(ctx, missing, wm)
	if err == nil || !strings.Contains(err.Error(), service.WebSocketInputServiceName) {
		t.Fatalf("missing service error = %v", err)
	}

	wrongType := manager.NewServiceManager("test")
	wrongType.RegisterService(appTestService{name: service.WebSocketInputServiceName})
	err = setupMessageListeners(ctx, wrongType, wm)
	if err == nil || !strings.Contains(err.Error(), "not of type") {
		t.Fatalf("wrong type error = %v", err)
	}
}

func TestAppStopReturnsServiceShutdownError(t *testing.T) {
	sm := manager.NewServiceManager("test")
	sm.RegisterService(appFailingStopService{appTestService{name: "failing"}})
	application := &App{serviceManager: sm, started: true}

	err := application.Stop(context.Background())
	if err == nil || !strings.Contains(err.Error(), "stop services") {
		t.Fatalf("stop error = %v, want service shutdown error", err)
	}
	if !application.started {
		t.Fatalf("failed stop cleared lifecycle state")
	}
}

func TestAppStartCleansUpWhenWorkflowConfigIsInvalid(t *testing.T) {
	wsServer, _ := newTestWebSocketServer(t)
	defer wsServer.Close()

	confluenceServer := newTestConfluenceServer(t)
	defer confluenceServer.Close()

	posts := make(chan string, 1)
	mattermostServer := newTestMattermostServer(t, posts)
	defer mattermostServer.Close()

	t.Setenv("WEBSOCKET_SERVER_URL", httpToWS(wsServer.URL))
	t.Setenv("CONFLUENCE_API_ENDPOINT", confluenceServer.URL)
	t.Setenv("MATTERMOST_SERVER_URL", mattermostServer.URL)
	t.Setenv("MATTERMOST_WS_URL", "ws://127.0.0.1:1")

	dir := t.TempDir()
	badConfigPath := filepath.Join(dir, "bad-rule-engine.yaml")
	if err := os.WriteFile(badConfigPath, []byte("workflows: ["), 0o644); err != nil {
		t.Fatalf("write bad config: %v", err)
	}

	cfg := config.DefaultRuntimeConfig()
	cfg.ApplyEnv()
	cfg.Admin.Address = "127.0.0.1:0"

	application := New("test-service-workflow", cfg, badConfigPath, "testdata/workflow-rules.yaml")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := application.Start(ctx)
	if err == nil {
		t.Fatalf("start error = nil, want workflow config error")
	}
	if !strings.Contains(err.Error(), "failed to initialize workflow manager") {
		t.Fatalf("error = %q, want workflow manager initialization failure", err.Error())
	}
}

func TestAppStartsWhenWebSocketInputIsDisabled(t *testing.T) {
	dir := t.TempDir()
	ruleConfigPath := filepath.Join(dir, "rule-engine.yaml")
	rulesPath := filepath.Join(dir, "workflow-rules.yaml")
	if err := os.WriteFile(ruleConfigPath, []byte(`
workflows:
  - name: WorkflowEngine
    providers: [yaml]
    mode: single
`), 0o644); err != nil {
		t.Fatalf("write rule config: %v", err)
	}
	if err := os.WriteFile(rulesPath, []byte(`version: "test"`), 0o644); err != nil {
		t.Fatalf("write rules: %v", err)
	}

	cfg := config.DefaultRuntimeConfig()
	cfg.Admin.Address = "127.0.0.1:0"
	cfg.Inputs.WebSocket.Enabled = false
	cfg.Adapters.Confluence.Enabled = false
	cfg.Adapters.Mattermost.Enabled = false
	cfg.Adapters.BadgeDB.Enabled = false

	application := New("test-service-workflow", cfg, ruleConfigPath, rulesPath)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := application.Start(ctx); err != nil {
		t.Fatalf("start app with websocket disabled: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := application.Stop(stopCtx); err != nil {
			t.Fatalf("stop app: %v", err)
		}
	}()

	resp, err := http.Get("http://" + application.adminServer.Addr() + "/services")
	if err != nil {
		t.Fatalf("get services: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("services status = %d, want 200", resp.StatusCode)
	}
}
