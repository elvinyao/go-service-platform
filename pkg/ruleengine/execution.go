package ruleengine

import (
	"fmt"
	"time"
)

// ExecutionPolicy is the resolved timeout and retry behavior for one action.
type ExecutionPolicy struct {
	// Timeout bounds each attempt. Zero means no limit.
	Timeout     time.Duration
	MaxAttempts int
	Backoff     time.Duration
	MaxBackoff  time.Duration
}

// ResolveExecution merges engine defaults with an action's own timeout and
// retry settings. An action-level retry block replaces the engine policy.
func (c EngineConfig) ResolveExecution(action Action) (ExecutionPolicy, error) {
	timeout := c.Execution.Timeout
	if action.Timeout != "" {
		timeout = action.Timeout
	}
	retry := c.Execution.Retry
	if action.Retry != nil {
		retry = *action.Retry
	}

	policy := ExecutionPolicy{MaxAttempts: retry.MaxAttempts}
	if policy.MaxAttempts == 0 {
		policy.MaxAttempts = 1
	}
	var err error
	if policy.Timeout, err = parseOptionalDuration("timeout", timeout); err != nil {
		return ExecutionPolicy{}, err
	}
	if policy.Backoff, err = parseOptionalDuration("retry.backoff", retry.Backoff); err != nil {
		return ExecutionPolicy{}, err
	}
	if policy.MaxBackoff, err = parseOptionalDuration("retry.max_backoff", retry.MaxBackoff); err != nil {
		return ExecutionPolicy{}, err
	}
	return policy, nil
}

func validateExecutionSettings(prefix, timeout string, retry *RetryPolicy) error {
	if _, err := parseOptionalDuration(prefix+".timeout", timeout); err != nil {
		return err
	}
	if retry == nil {
		return nil
	}
	if retry.MaxAttempts < 0 {
		return fmt.Errorf("%s.retry.max_attempts must not be negative", prefix)
	}
	if _, err := parseOptionalDuration(prefix+".retry.backoff", retry.Backoff); err != nil {
		return err
	}
	if _, err := parseOptionalDuration(prefix+".retry.max_backoff", retry.MaxBackoff); err != nil {
		return err
	}
	return nil
}

func parseOptionalDuration(name, value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s %q is not a valid duration", name, value)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%s %q must not be negative", name, value)
	}
	return parsed, nil
}
