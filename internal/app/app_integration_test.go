package app

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"project/internal/model"
	"project/pkg/config"

	"github.com/gorilla/websocket"
)

func TestAppProcessesWebSocketMessageThroughRulesAndExecutors(t *testing.T) {
	wsServer, connections := newTestWebSocketServer(t)
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

	cfg := config.DefaultRuntimeConfig()
	cfg.Admin.Address = "127.0.0.1:0"

	application := New("test-service-workflow", cfg, "testdata/rule-engine.yaml", "testdata/workflow-rules.yaml")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := application.Start(ctx); err != nil {
		t.Fatalf("start app: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := application.Stop(stopCtx); err != nil {
			t.Fatalf("stop app: %v", err)
		}
	}()

	conn := waitForWebSocketConnection(t, connections)
	if err := conn.WriteJSON(model.Message{
		ID:      "m1",
		Type:    "AAA",
		Content: "hello integration",
		UserID:  "u1",
	}); err != nil {
		t.Fatalf("write websocket message: %v", err)
	}

	select {
	case post := <-posts:
		if !strings.Contains(post, "Event AAA received: hello integration") {
			t.Fatalf("mattermost post = %q", post)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for mattermost post")
	}

	ruleEngineURL := "http://" + application.adminServer.Addr() + "/rule-engine"
	resp, err := http.Get(ruleEngineURL)
	if err != nil {
		t.Fatalf("get rule engine admin endpoint: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rule engine status = %d, want 200", resp.StatusCode)
	}
}

func TestAppAdminEndpointsReturnContracts(t *testing.T) {
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

	cfg := config.DefaultRuntimeConfig()
	cfg.Admin.Address = "127.0.0.1:0"

	application := New("test-service-workflow", cfg, "testdata/rule-engine.yaml", "testdata/workflow-rules.yaml")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := application.Start(ctx); err != nil {
		t.Fatalf("start app: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := application.Stop(stopCtx); err != nil {
			t.Fatalf("stop app: %v", err)
		}
	}()

	baseURL := "http://" + application.adminServer.Addr()

	var healthResponse struct {
		ServiceName string `json:"service_name"`
		Status      string `json:"status"`
		Version     string `json:"version"`
	}
	getJSON(t, baseURL+"/health", &healthResponse)
	if healthResponse.ServiceName != "system" {
		t.Fatalf("health service_name = %q, want system", healthResponse.ServiceName)
	}
	if healthResponse.Status == "" {
		t.Fatalf("health status is empty")
	}

	var servicesResponse []struct {
		Name    string `json:"name"`
		Running bool   `json:"running"`
		Type    string `json:"type"`
	}
	getJSON(t, baseURL+"/services", &servicesResponse)
	if !containsService(servicesResponse, "WebSocketServiceA") {
		t.Fatalf("services response missing WebSocketServiceA: %+v", servicesResponse)
	}
	if !containsService(servicesResponse, "ConfluenceSettingsService") {
		t.Fatalf("services response missing ConfluenceSettingsService: %+v", servicesResponse)
	}

	var workflowsResponse []struct {
		Name string `json:"name"`
	}
	getJSON(t, baseURL+"/workflows", &workflowsResponse)
	if len(workflowsResponse) != 1 || workflowsResponse[0].Name != "WorkflowEngine" {
		t.Fatalf("workflows response = %+v, want WorkflowEngine", workflowsResponse)
	}

	var ruleEngineResponse struct {
		Name      string   `json:"name"`
		Executors []string `json:"executors"`
		Composer  struct {
			Providers map[string]json.RawMessage `json:"providers"`
		} `json:"composer"`
	}
	getJSON(t, baseURL+"/rule-engine", &ruleEngineResponse)
	if ruleEngineResponse.Name != "WorkflowEngine" {
		t.Fatalf("rule engine name = %q, want WorkflowEngine", ruleEngineResponse.Name)
	}
	if !containsString(ruleEngineResponse.Executors, "log") || !containsString(ruleEngineResponse.Executors, "mattermost") {
		t.Fatalf("executors = %+v, want log and mattermost", ruleEngineResponse.Executors)
	}
	if _, ok := ruleEngineResponse.Composer.Providers["yaml"]; !ok {
		t.Fatalf("providers missing yaml: %+v", ruleEngineResponse.Composer.Providers)
	}
	if _, ok := ruleEngineResponse.Composer.Providers["confluence"]; !ok {
		t.Fatalf("providers missing confluence: %+v", ruleEngineResponse.Composer.Providers)
	}
}

func TestAppStartFailsWhenAdminAddressIsInUse(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	t.Setenv("WEBSOCKET_SERVER_URL", "ws://127.0.0.1:1")
	t.Setenv("CONFLUENCE_API_ENDPOINT", "http://127.0.0.1:1")
	t.Setenv("MATTERMOST_SERVER_URL", "http://127.0.0.1:1")
	t.Setenv("MATTERMOST_WS_URL", "ws://127.0.0.1:1")

	cfg := config.DefaultRuntimeConfig()
	cfg.Admin.Address = listener.Addr().String()

	application := New("test-service-workflow", cfg, "testdata/rule-engine.yaml", "testdata/workflow-rules.yaml")
	err = application.Start(context.Background())
	if err == nil {
		t.Fatalf("start app error = nil, want admin bind error")
	}
	if !strings.Contains(err.Error(), "failed to start admin server") {
		t.Fatalf("error = %q, want failed to start admin server", err.Error())
	}
}

func newTestWebSocketServer(t *testing.T) (*httptest.Server, <-chan *websocket.Conn) {
	t.Helper()

	upgrader := websocket.Upgrader{}
	connections := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		connections <- conn
	}))

	return server, connections
}

func newTestConfluenceServer(t *testing.T) *httptest.Server {
	t.Helper()

	settings := map[string]interface{}{
		"rules": []map[string]interface{}{
			{
				"event_type":       "AAA",
				"pattern":          ".*",
				"channel_id":       "test-channel-1",
				"message_template": "Event AAA received: {{.Content}}",
				"enabled":          true,
			},
		},
	}
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/content/settings-page-1" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    "settings-page-1",
			"title": "Workflow Settings",
			"body": map[string]interface{}{
				"storage": map[string]interface{}{
					"value": string(settingsJSON),
				},
			},
		})
	}))
}

func newTestMattermostServer(t *testing.T, posts chan<- string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/posts" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}

		var body struct {
			ChannelID string `json:"channel_id"`
			Message   string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode mattermost post: %v", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		select {
		case posts <- body.Message:
		default:
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":         "post-id",
			"channel_id": body.ChannelID,
			"message":    body.Message,
		})
	}))
}

func waitForWebSocketConnection(t *testing.T, connections <-chan *websocket.Conn) *websocket.Conn {
	t.Helper()

	select {
	case conn := <-connections:
		return conn
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for websocket connection")
		return nil
	}
}

func httpToWS(rawURL string) string {
	return "ws" + strings.TrimPrefix(rawURL, "http")
}

func getJSON(t *testing.T, url string, target interface{}) {
	t.Helper()

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", url, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(target); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
}

func containsService(services []struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
	Type    string `json:"type"`
}, name string) bool {
	for _, svc := range services {
		if svc.Name == name && svc.Running && svc.Type != "" {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
