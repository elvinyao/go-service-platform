package badgedb

import (
	"context"
	"strings"
	"testing"

	"project/internal/manager"
	"project/internal/service"
	"project/pkg/ruleengine"
)

func TestExecutorType(t *testing.T) {
	exe := NewExecutor(nil)
	if exe.Type() != "db" {
		t.Fatalf("type = %q, want db", exe.Type())
	}
}

func TestExecutorSavesBadgeWithActionParams(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sm := manager.NewServiceManager("test")
	svc := service.NewBadgeDBService("BadgeDBService", "Global", "", "test")
	sm.RegisterService(svc)
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("start badgedb service: %v", err)
	}
	defer svc.Stop(context.Background())

	err := NewExecutor(sm).Execute(ctx, ruleengine.Message{
		ID:     "m1",
		Type:   "AAA",
		UserID: "u1",
	}, ruleengine.Action{
		ID:       "save",
		Executor: "db",
		Params: map[string]interface{}{
			"name":        "Debug Badge",
			"description": "Created in test",
			"badge_type":  "debug",
		},
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	metrics := svc.GetMetrics(ctx)
	if metrics["db_entries"] != 1 {
		t.Fatalf("db_entries = %v, want 1", metrics["db_entries"])
	}
}

func TestExecutorReturnsErrorWhenBadgeDBServiceMissing(t *testing.T) {
	err := NewExecutor(manager.NewServiceManager("test")).Execute(context.Background(), ruleengine.Message{}, ruleengine.Action{})
	if err == nil {
		t.Fatalf("expected missing service error")
	}
	if !strings.Contains(err.Error(), "BadgeDBService not found") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestExecutorReturnsErrorWhenBadgeDBServiceStopped(t *testing.T) {
	sm := manager.NewServiceManager("test")
	svc := service.NewBadgeDBService("BadgeDBService", "Global", "", "test")
	sm.RegisterService(svc)

	err := NewExecutor(sm).Execute(context.Background(), ruleengine.Message{ID: "m1"}, ruleengine.Action{})
	if err == nil {
		t.Fatalf("expected stopped service error")
	}
	if !strings.Contains(err.Error(), "service not running") {
		t.Fatalf("error = %q", err.Error())
	}
}
