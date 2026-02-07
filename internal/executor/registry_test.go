package executor

import (
	"context"
	"testing"

	"project/internal/model"
	"project/internal/ruleengine"

	"github.com/stretchr/testify/require"
)

type dummyExecutor struct {
	name string
}

func (d *dummyExecutor) Type() string { return d.name }
func (d *dummyExecutor) Execute(ctx context.Context, msg model.Message, action ruleengine.Action) error {
	return nil
}

func TestRegistryListTypesSorted(t *testing.T) {
	registry := NewRegistry()
	require.NoError(t, registry.Register(&dummyExecutor{name: "mattermost"}))
	require.NoError(t, registry.Register(&dummyExecutor{name: "db"}))
	require.NoError(t, registry.Register(&dummyExecutor{name: "log"}))

	types := registry.ListTypes()
	require.Equal(t, []string{"db", "log", "mattermost"}, types)
}
