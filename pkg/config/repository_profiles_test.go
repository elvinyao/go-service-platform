package config_test

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/elvinyao/go-service-platform/pkg/config"
	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

func TestRepositoryProfilesLoadWithExpectedCapabilities(t *testing.T) {
	repositoryRoot := filepath.Join("..", "..")
	tests := []struct {
		name              string
		runtimePath       string
		enginePath        string
		providers         []string
		confluenceEnabled bool
		mattermostEnabled bool
		badgeDBEnabled    bool
	}{
		{
			name:        "base",
			runtimePath: "config/runtime.yaml",
			enginePath:  "config/rule-engine.yaml",
			providers:   []string{"yaml"},
		},
		{
			name:              "demo",
			runtimePath:       "config/profiles/demo/runtime.yaml",
			enginePath:        "config/profiles/demo/rule-engine.yaml",
			providers:         []string{"yaml", "confluence"},
			confluenceEnabled: true,
			mattermostEnabled: true,
			badgeDBEnabled:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runtimeCfg, err := config.LoadRuntimeConfig(filepath.Join(repositoryRoot, tt.runtimePath))
			if err != nil {
				t.Fatalf("load runtime config: %v", err)
			}
			if runtimeCfg.Admin.Address != "127.0.0.1:18080" {
				t.Fatalf("admin address = %q, want 127.0.0.1:18080", runtimeCfg.Admin.Address)
			}
			if runtimeCfg.Adapters.Confluence.Enabled != tt.confluenceEnabled ||
				runtimeCfg.Adapters.Mattermost.Enabled != tt.mattermostEnabled ||
				runtimeCfg.Adapters.BadgeDB.Enabled != tt.badgeDBEnabled {
				t.Fatalf("adapter flags = %+v", runtimeCfg.Adapters)
			}

			engineCfg, err := ruleengine.LoadEngineConfig(filepath.Join(repositoryRoot, tt.enginePath))
			if err != nil {
				t.Fatalf("load engine config: %v", err)
			}
			if got := engineCfg.Workflows[0].Providers; !slices.Equal(got, tt.providers) {
				t.Fatalf("providers = %+v, want %+v", got, tt.providers)
			}
		})
	}
}

func TestRepositoryWorkflowRulesLoad(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		executors []string
	}{
		{name: "base", path: "config/workflow-rules.yaml", executors: []string{"log"}},
		{name: "demo", path: "config/profiles/demo/workflow-rules.yaml", executors: []string{"db", "log"}},
		{name: "im-bot example", path: "examples/rules/im-bot.yaml", executors: []string{"log", "reply"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := ruleengine.NewYAMLProvider(
				"yaml",
				filepath.Join("..", "..", tt.path),
				ruleengine.DefaultWorkflowName,
			)
			if err := provider.Start(context.Background()); err != nil {
				t.Fatalf("load repository workflow rules: %v", err)
			}

			var executors []string
			for _, rule := range provider.Snapshot(context.Background()).Rules {
				for _, action := range rule.Actions {
					if !slices.Contains(executors, action.Executor) {
						executors = append(executors, action.Executor)
					}
				}
			}
			slices.Sort(executors)
			if !slices.Equal(executors, tt.executors) {
				t.Fatalf("executors = %+v, want %+v", executors, tt.executors)
			}
		})
	}
}
