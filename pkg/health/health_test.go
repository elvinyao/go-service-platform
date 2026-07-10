package health

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCheckResult(t *testing.T) {
	// Test creating a new CheckResult
	t.Run("NewCheckResult", func(t *testing.T) {
		cr := NewCheckResult("database", CategoryDependency)
		assert.Equal(t, "database", cr.Name)
		assert.Equal(t, CategoryDependency, cr.Category)
		assert.Equal(t, StatusUnknown, cr.Status)
		assert.Empty(t, cr.Description)
		assert.NotNil(t, cr.Details)
		assert.False(t, cr.Timestamp.IsZero())
	})

	// Test setting status
	t.Run("SetStatus", func(t *testing.T) {
		cr := NewCheckResult("cache", CategoryResources)
		cr.SetStatus(StatusUp, "Cache is working properly")
		assert.Equal(t, StatusUp, cr.Status)
		assert.Equal(t, "Cache is working properly", cr.Description)
	})

	// Test adding details
	t.Run("AddDetail", func(t *testing.T) {
		cr := NewCheckResult("memory", CategoryResources)
		cr.AddDetail("total", "8GB")
		cr.AddDetail("used", "4GB")
		cr.AddDetail("free", "4GB")

		assert.Len(t, cr.Details, 3)
		assert.Equal(t, "8GB", cr.Details["total"])
		assert.Equal(t, "4GB", cr.Details["used"])
		assert.Equal(t, "4GB", cr.Details["free"])
	})

	// Test Complete method
	t.Run("Complete", func(t *testing.T) {
		cr := NewCheckResult("api", CategoryConnectivity)
		time.Sleep(5 * time.Millisecond) // Ensure some time passes
		cr.Complete()

		assert.True(t, cr.Duration > 0)
	})

	// Test IsHealthy method
	t.Run("IsHealthy", func(t *testing.T) {
		upResult := NewCheckResult("service-up", CategoryDependency)
		upResult.SetStatus(StatusUp, "Service is up")
		assert.True(t, upResult.IsHealthy())

		degradedResult := NewCheckResult("service-degraded", CategoryDependency)
		degradedResult.SetStatus(StatusDegraded, "Service is degraded")
		assert.False(t, degradedResult.IsHealthy())

		downResult := NewCheckResult("service-down", CategoryDependency)
		downResult.SetStatus(StatusDown, "Service is down")
		assert.False(t, downResult.IsHealthy())

		unknownResult := NewCheckResult("service-unknown", CategoryDependency)
		assert.False(t, unknownResult.IsHealthy())
	})
}

func TestReport(t *testing.T) {
	// Test creating a new Report
	t.Run("NewReport", func(t *testing.T) {
		startTime := time.Now().Add(-1 * time.Hour)
		report := NewReport("test-service", startTime, "1.0.0")

		assert.Equal(t, "test-service", report.ServiceName)
		assert.Equal(t, StatusUnknown, report.Status)
		assert.Empty(t, report.CheckResults)
		assert.Equal(t, startTime, report.StartTime)
		assert.InDelta(t, time.Hour.Seconds(), report.Uptime.Seconds(), 1)
		assert.Equal(t, "1.0.0", report.Version)
		assert.NotNil(t, report.Metadata)
	})

	// Test adding results
	t.Run("AddResult", func(t *testing.T) {
		report := NewReport("test-service", time.Now(), "1.0.0")

		cr1 := NewCheckResult("check1", CategoryConnectivity)
		cr1.SetStatus(StatusUp, "Check 1 passed")

		cr2 := NewCheckResult("check2", CategoryResources)
		cr2.SetStatus(StatusDown, "Check 2 failed")

		report.AddResult(*cr1)
		report.AddResult(*cr2)

		assert.Len(t, report.CheckResults, 2)
		assert.Equal(t, "check1", report.CheckResults[0].Name)
		assert.Equal(t, "check2", report.CheckResults[1].Name)
	})

	// Test Complete method
	t.Run("Complete", func(t *testing.T) {
		report := NewReport("test-service", time.Now(), "1.0.0")

		cr1 := NewCheckResult("check1", CategoryConnectivity)
		cr1.SetStatus(StatusUp, "Check 1 passed")

		cr2 := NewCheckResult("check2", CategoryResources)
		cr2.SetStatus(StatusDown, "Check 2 failed")

		report.AddResult(*cr1)
		report.AddResult(*cr2)

		time.Sleep(5 * time.Millisecond) // Ensure some time passes
		report.Complete()

		assert.Equal(t, StatusDegraded, report.Status)
		assert.True(t, report.RefreshElapsed > 0)
		assert.False(t, report.RefreshedAt.IsZero())
	})

	// Test IsHealthy method
	t.Run("IsHealthy", func(t *testing.T) {
		// Report with all checks UP
		upReport := NewReport("up-service", time.Now(), "1.0.0")
		upCheck := NewCheckResult("check", CategoryConnectivity)
		upCheck.SetStatus(StatusUp, "Check passed")
		upReport.AddResult(*upCheck)
		upReport.Complete()
		assert.True(t, upReport.IsHealthy())

		// Report with a DOWN check
		downReport := NewReport("down-service", time.Now(), "1.0.0")
		downCheck := NewCheckResult("check", CategoryConnectivity)
		downCheck.SetStatus(StatusDown, "Check failed")
		downReport.AddResult(*downCheck)
		downReport.Complete()
		assert.False(t, downReport.IsHealthy())

		// Report with DEGRADED check
		degradedReport := NewReport("degraded-service", time.Now(), "1.0.0")
		degradedCheck := NewCheckResult("check", CategoryConnectivity)
		degradedCheck.SetStatus(StatusDegraded, "Service degraded")
		degradedReport.AddResult(*degradedCheck)
		degradedReport.Complete()
		assert.False(t, degradedReport.IsHealthy())

		// Empty report
		emptyReport := NewReport("empty-service", time.Now(), "1.0.0")
		emptyReport.Complete()
		assert.True(t, emptyReport.Status == StatusUnknown)
		assert.False(t, emptyReport.IsHealthy())
	})
}

func TestDetermineStatus(t *testing.T) {
	// All checks UP
	t.Run("AllUp", func(t *testing.T) {
		results := []CheckResult{
			{Status: StatusUp},
			{Status: StatusUp},
			{Status: StatusUp},
		}
		status := DetermineStatus(results)
		assert.Equal(t, StatusUp, status)
	})

	// One critical DOWN
	t.Run("OneCriticalDown", func(t *testing.T) {
		results := []CheckResult{
			{Status: StatusUp, Level: LevelInfo},
			{Status: StatusDown, Level: LevelCritical},
			{Status: StatusUp, Level: LevelWarning},
		}
		status := DetermineStatus(results)
		assert.Equal(t, StatusDown, status)
	})

	// All warnings DOWN
	t.Run("AllWarningsDown", func(t *testing.T) {
		results := []CheckResult{
			{Status: StatusDown, Level: LevelWarning},
			{Status: StatusDown, Level: LevelWarning},
		}
		status := DetermineStatus(results)
		assert.Equal(t, StatusDegraded, status)
	})

	// Mix of UP and DEGRADED
	t.Run("MixedUpAndDegraded", func(t *testing.T) {
		results := []CheckResult{
			{Status: StatusUp, Level: LevelInfo},
			{Status: StatusDegraded, Level: LevelWarning},
			{Status: StatusUp, Level: LevelCritical},
		}
		status := DetermineStatus(results)
		assert.Equal(t, StatusDegraded, status)
	})

	// Empty results
	t.Run("EmptyResults", func(t *testing.T) {
		status := DetermineStatus([]CheckResult{})
		assert.Equal(t, StatusUnknown, status)
	})
}

func TestRunChecksParallel(t *testing.T) {
	ctx := context.Background()

	// Create some test check functions
	check1 := func(ctx context.Context) *CheckResult {
		cr := NewCheckResult("check1", CategoryConnectivity)
		cr.SetStatus(StatusUp, "Check 1 passed")
		return cr
	}

	check2 := func(ctx context.Context) *CheckResult {
		cr := NewCheckResult("check2", CategoryResources)
		cr.SetStatus(StatusDown, "Check 2 failed")
		return cr
	}

	check3 := func(ctx context.Context) *CheckResult {
		time.Sleep(50 * time.Millisecond) // Simulate a slow check
		cr := NewCheckResult("check3", CategoryDependency)
		cr.SetStatus(StatusDegraded, "Check 3 degraded")
		return cr
	}

	checks := []func(context.Context) *CheckResult{check1, check2, check3}

	// Run the checks in parallel
	start := time.Now()
	results := RunChecksParallel(ctx, checks)
	elapsed := time.Since(start)

	// Verify results
	assert.Len(t, results, 3)
	assert.True(t, elapsed < 100*time.Millisecond, "Parallel execution should take less than 100ms")

	// Verify the correct results were returned
	names := make(map[string]bool)
	for _, result := range results {
		names[result.Name] = true

		// Check that each result has the expected status
		switch result.Name {
		case "check1":
			assert.Equal(t, StatusUp, result.Status)
		case "check2":
			assert.Equal(t, StatusDown, result.Status)
		case "check3":
			assert.Equal(t, StatusDegraded, result.Status)
		}
	}

	assert.True(t, names["check1"])
	assert.True(t, names["check2"])
	assert.True(t, names["check3"])
}

func TestRunChecksParallelContainsExtensionFailures(t *testing.T) {
	checks := []func(context.Context) *CheckResult{
		func(context.Context) *CheckResult {
			time.Sleep(10 * time.Millisecond)
			result := NewCheckResult("first", CategoryDependency)
			result.SetStatus(StatusUp, "")
			return result
		},
		func(context.Context) *CheckResult {
			result := NewCheckResult("second", CategoryDependency)
			result.SetStatus(StatusUp, "")
			return result
		},
		nil,
		func(context.Context) *CheckResult { panic("boom") },
		func(context.Context) *CheckResult { return nil },
	}

	results := RunChecksParallel(nil, checks)

	assert.Equal(t, []string{"first", "second", "invalid", "panic", "invalid"}, checkResultNames(results))
	assert.Equal(t, StatusUnknown, results[2].Status)
	assert.Contains(t, results[3].Description, "boom")
}

func TestRunChecksParallelTimesOutUnfinishedChecks(t *testing.T) {
	results := runChecksParallel(context.Background(), 10*time.Millisecond, []func(context.Context) *CheckResult{
		func(ctx context.Context) *CheckResult {
			<-ctx.Done()
			return NewCheckResult("late", CategoryDependency)
		},
	})

	assert.Len(t, results, 1)
	assert.Equal(t, "timeout", results[0].Name)
	assert.Equal(t, StatusUnknown, results[0].Status)
}

func checkResultNames(results []*CheckResult) []string {
	names := make([]string, len(results))
	for i, result := range results {
		names[i] = result.Name
	}
	return names
}
