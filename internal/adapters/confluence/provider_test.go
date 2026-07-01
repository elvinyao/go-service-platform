package confluence

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	internalconfig "project/internal/config"
	"project/internal/dataaccess"
	"project/internal/manager"
	"project/internal/service"
	"project/pkg/ruleengine"
)

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

func TestProviderSnapshotInvalidRegexDoesNotMatchMessage(t *testing.T) {
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
	matches := ruleengine.MatchRules(snapshot.Rules, ruleengine.Message{Type: "AAA", Content: "anything"})

	if len(matches) != 0 {
		t.Fatalf("matches len = %d, want 0 for invalid regex", len(matches))
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

func newConfluenceServiceManager(t *testing.T, rules []service.SettingRule) (*manager.ServiceManager, func()) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/api/content/settings-page-1" {
			http.NotFound(w, r)
			return
		}

		body, err := json.Marshal(map[string]interface{}{"rules": rules})
		if err != nil {
			t.Fatalf("marshal rules: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    "settings-page-1",
			"title": "Workflow Settings",
			"body": map[string]interface{}{
				"storage": map[string]interface{}{
					"value": string(body),
				},
			},
		})
	}))

	ctx := context.Background()
	sm := manager.NewServiceManager("test")
	svc := service.NewConfluenceSettingsService(
		"ConfluenceSettingsService",
		"WorkflowEngine",
		dataaccess.NewCacheDataAccessor(ctx),
		internalconfig.ConfluenceSettingsConfig{
			PageID:          "settings-page-1",
			RefreshInterval: time.Hour,
			APIEndpoint:     server.URL,
			SpaceKey:        "TEST",
		},
		"test",
	)
	sm.RegisterService(svc)
	if err := svc.Start(ctx); err != nil {
		server.Close()
		t.Fatalf("start settings service: %v", err)
	}

	return sm, func() {
		_ = svc.Stop(context.Background())
		server.Close()
	}
}
