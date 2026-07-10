package confluence

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	internalconfig "github.com/elvinyao/go-service-platform/internal/config"
	"github.com/elvinyao/go-service-platform/internal/dataaccess"
	"github.com/elvinyao/go-service-platform/internal/manager"
	"github.com/elvinyao/go-service-platform/internal/service"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

type confluenceRoundTripFunc func(*http.Request) (*http.Response, error)

func (f confluenceRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestProviderSnapshotMapsSettingsToRules(t *testing.T) {
	ctx := context.Background()
	sm, cleanup := newConfluenceServiceManager(t, []service.SettingRule{
		{
			EventType:   "AAA",
			Pattern:     "urgent.*",
			ChannelID:   "alerts",
			MessageTmpl: "Event {{.Content}}",
			Enabled:     true,
		},
		{
			EventType:   "BBB",
			Pattern:     ".*",
			ChannelID:   "disabled",
			MessageTmpl: "Disabled",
			Enabled:     false,
		},
	})
	defer cleanup()

	provider := NewProvider("confluence", ruleengine.DefaultWorkflowName, sm)
	snapshot := provider.Snapshot(ctx)

	if snapshot.LastError != "" {
		t.Fatalf("last error = %q, want empty", snapshot.LastError)
	}
	if len(snapshot.Rules) != 2 {
		t.Fatalf("rules len = %d, want 2", len(snapshot.Rules))
	}
	rule := snapshot.Rules[0]
	if rule.ID != "cf_rule_0_AAA" || rule.Workflow != ruleengine.DefaultWorkflowName || rule.Source != "confluence" {
		t.Fatalf("rule metadata = %+v", rule)
	}
	if !rule.Enabled {
		t.Fatalf("rule enabled = false, want true")
	}
	if len(rule.Conditions) != 2 {
		t.Fatalf("conditions len = %d, want 2", len(rule.Conditions))
	}
	if rule.Actions[0].Executor != "mattermost" {
		t.Fatalf("executor = %q, want mattermost", rule.Actions[0].Executor)
	}
	if rule.Actions[0].Params["channel_id"] != "alerts" {
		t.Fatalf("channel = %v, want alerts", rule.Actions[0].Params["channel_id"])
	}
	if snapshot.Rules[1].Enabled {
		t.Fatalf("disabled setting produced enabled rule")
	}
}

func TestProviderNameAndStartUseCurrentSnapshot(t *testing.T) {
	ctx := context.Background()
	sm, cleanup := newConfluenceServiceManager(t, []service.SettingRule{
		{
			EventType:   "AAA",
			ChannelID:   "alerts",
			MessageTmpl: "Event {{.Content}}",
			Enabled:     true,
		},
	})
	defer cleanup()

	provider := NewProvider("confluence", ruleengine.DefaultWorkflowName, sm)
	if provider.Name() != "confluence" {
		t.Fatalf("name = %q, want confluence", provider.Name())
	}
	if err := provider.Start(ctx); err != nil {
		t.Fatalf("start provider: %v", err)
	}

	svc, ok := sm.GetServiceByName(ctx, "ConfluenceSettingsService")
	if !ok {
		t.Fatalf("settings service not found")
	}
	if err := svc.Stop(ctx); err != nil {
		t.Fatalf("stop settings service: %v", err)
	}

	snapshot := provider.Snapshot(ctx)
	if snapshot.LastError == "" {
		t.Fatalf("last error = empty, want stopped service error")
	}
	if len(snapshot.Rules) != 1 {
		t.Fatalf("rules len = %d, want Start to preserve last good snapshot", len(snapshot.Rules))
	}
}

func TestProviderSnapshotInvalidRegexUsesLastGood(t *testing.T) {
	ctx := context.Background()
	sm, cleanup := newConfluenceServiceManager(t, []service.SettingRule{
		{
			EventType:   "AAA",
			Pattern:     "[",
			ChannelID:   "alerts",
			MessageTmpl: "Event {{.Content}}",
			Enabled:     true,
		},
	})
	defer cleanup()

	provider := NewProvider("confluence", ruleengine.DefaultWorkflowName, sm)
	snapshot := provider.Snapshot(ctx)

	if len(snapshot.Rules) != 0 || !strings.Contains(snapshot.LastError, "regex is invalid") {
		t.Fatalf("invalid snapshot = %+v, want empty last-good with validation error", snapshot)
	}
}

func TestProviderSnapshotUsesLastGoodWhenSettingsServiceStops(t *testing.T) {
	ctx := context.Background()
	sm, cleanup := newConfluenceServiceManager(t, []service.SettingRule{
		{
			EventType:   "AAA",
			Pattern:     ".*",
			ChannelID:   "alerts",
			MessageTmpl: "Event {{.Content}}",
			Enabled:     true,
		},
	})
	defer cleanup()

	provider := NewProvider("confluence", ruleengine.DefaultWorkflowName, sm)
	first := provider.Snapshot(ctx)
	if len(first.Rules) != 1 {
		t.Fatalf("first rules len = %d, want 1", len(first.Rules))
	}

	svc, ok := sm.GetServiceByName(ctx, "ConfluenceSettingsService")
	if !ok {
		t.Fatalf("settings service not found")
	}
	if err := svc.Stop(ctx); err != nil {
		t.Fatalf("stop settings service: %v", err)
	}

	second := provider.Snapshot(ctx)
	if second.LastError == "" {
		t.Fatalf("last error = empty, want service stopped error")
	}
	if len(second.Rules) != 1 {
		t.Fatalf("second rules len = %d, want last good snapshot with 1 rule", len(second.Rules))
	}
	if second.Rules[0].ID != first.Rules[0].ID {
		t.Fatalf("second rule ID = %q, want %q", second.Rules[0].ID, first.Rules[0].ID)
	}
}

func TestProviderSnapshotReturnsLastErrorWhenServiceMissing(t *testing.T) {
	provider := NewProvider("confluence", ruleengine.DefaultWorkflowName, manager.NewServiceManager("test"))
	snapshot := provider.Snapshot(context.Background())

	if snapshot.LastError == "" {
		t.Fatalf("last error = empty, want missing service error")
	}
	if len(snapshot.Rules) != 0 {
		t.Fatalf("rules len = %d, want 0", len(snapshot.Rules))
	}
}

func TestProviderLastGoodSnapshotIsDeeplyIsolated(t *testing.T) {
	provider := NewProvider("confluence", ruleengine.DefaultWorkflowName, manager.NewServiceManager("test"))
	provider.lastGood = ruleengine.RuleSet{Rules: []ruleengine.Rule{{
		ID: "r1",
		Actions: []ruleengine.Action{{
			ID:       "a1",
			Executor: "mattermost",
			Params: map[string]interface{}{
				"headers": map[string]interface{}{"X-Test": "original"},
			},
		}},
	}}}

	first := provider.Snapshot(context.Background())
	first.Rules[0].Actions[0].Params["headers"].(map[string]interface{})["X-Test"] = "changed"
	second := provider.Snapshot(context.Background())
	if got := second.Rules[0].Actions[0].Params["headers"].(map[string]interface{})["X-Test"]; got != "original" {
		t.Fatalf("last-good header = %v, want original", got)
	}
}

func newConfluenceServiceManager(t *testing.T, rules []service.SettingRule) (*manager.ServiceManager, func()) {
	t.Helper()

	settingsBody, err := json.Marshal(map[string]interface{}{"rules": rules})
	if err != nil {
		t.Fatalf("marshal rules: %v", err)
	}
	responseBody, err := json.Marshal(map[string]interface{}{
		"id":    "settings-page-1",
		"title": "Workflow Settings",
		"body": map[string]interface{}{
			"storage": map[string]interface{}{
				"value": string(settingsBody),
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	client := &http.Client{Transport: confluenceRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		status := http.StatusOK
		body := responseBody
		if request.URL.Path != "/rest/api/content/settings-page-1" {
			status = http.StatusNotFound
			body = []byte("not found")
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(body)),
			Request:    request,
		}, nil
	})}

	ctx := context.Background()
	sm := manager.NewServiceManager("test")
	svc := service.NewConfluenceSettingsService(
		"ConfluenceSettingsService",
		"WorkflowEngine",
		dataaccess.NewCacheDataAccessor(ctx),
		internalconfig.ConfluenceSettingsConfig{
			PageID:          "settings-page-1",
			RefreshInterval: time.Hour,
			APIEndpoint:     "http://confluence.test",
			SpaceKey:        "TEST",
		},
		"test",
		client,
	)
	sm.RegisterService(svc)
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start settings service: %v", err)
	}

	return sm, func() {
		_ = svc.Stop(context.Background())
	}
}
