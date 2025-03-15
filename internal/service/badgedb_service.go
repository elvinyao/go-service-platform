package service

import (
	"context"
	"fmt"
	"project/internal/model"
	appctx "project/pkg/context"
	"project/pkg/health"
	"project/pkg/logger"
	"sync"
	"time"
	// 其他必要的导入
)

// BadgeDBService manages badge data
type BadgeDBService struct {
	*BaseService
	mu            sync.Mutex
	db            map[string]interface{}
	dbPath        string
	processingMap map[string]string
	lastBackup    time.Time
}

// NewBadgeDBService creates a new BadgeDBService
func NewBadgeDBService(name, workflow, dbPath, version string) *BadgeDBService {
	s := &BadgeDBService{
		BaseService:   NewBaseService(name, workflow, "badgedb", version),
		db:            make(map[string]interface{}),
		dbPath:        dbPath,
		processingMap: make(map[string]string),
		lastBackup:    time.Time{},
	}

	// Add custom health checkers
	s.AddHealthChecker(&badgeDBAccessChecker{service: s})
	s.AddHealthChecker(&badgeDBBackupChecker{service: s})

	return s
}

// Start implements Service interface with context support
func (s *BadgeDBService) Start(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "start_service")

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.IsRunning() {
		logger.InfofWithContext(ctx, "Service %s is already running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Starting service: %s", s.GetName())

	// Load database
	if err := s.loadDB(ctx); err != nil {
		return err
	}

	// Initialize processing map
	s.initProcessingMap()

	// Set service as running
	s.LockRunning(true)

	// Setup cleanup on context cancellation
	go func() {
		<-ctx.Done()
		stopCtx := appctx.NewContext(context.Background())
		if err := s.Stop(stopCtx); err != nil {
			logger.WithContextError(stopCtx, err).Error("Error stopping service on context cancellation")
		}
	}()

	return nil
}

// Stop implements Service interface with context support
func (s *BadgeDBService) Stop(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "stop_service")

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning() {
		logger.InfofWithContext(ctx, "Service %s is not running", s.GetName())
		return nil
	}

	logger.InfofWithContext(ctx, "Stopping service: %s", s.GetName())

	// Backup database before stopping
	if err := s.backupDB(ctx); err != nil {
		logger.WithContextError(ctx, err).Error("Failed to backup database during shutdown")
	}

	// Set service as not running
	s.LockRunning(false)

	return nil
}

// Restart implements Service interface with context support
func (s *BadgeDBService) Restart(ctx context.Context) error {
	ctx = appctx.WithOperationName(ctx, "restart_service")
	logger.InfofWithContext(ctx, "Restarting service: %s", s.GetName())

	if err := s.Stop(ctx); err != nil {
		return err
	}

	return s.Start(ctx)
}

// GetProcessingMethod returns the processing method for a message type
func (s *BadgeDBService) GetProcessingMethod(messageType string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	method, ok := s.processingMap[messageType]
	if !ok {
		return "", fmt.Errorf("no processing method found for message type: %s", messageType)
	}

	return method, nil
}

// GetBadge retrieves a badge by ID
func (s *BadgeDBService) GetBadge(ctx context.Context, id string) (model.Badge, error) {
	ctx = appctx.WithOperationName(ctx, "get_badge")
	logger.DebugfWithContext(ctx, "Getting badge with ID: %s", id)

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning() {
		return model.Badge{}, fmt.Errorf("service is not running")
	}

	data, ok := s.db[id]
	if !ok {
		return model.Badge{}, fmt.Errorf("badge not found: %s", id)
	}

	badge, ok := data.(model.Badge)
	if !ok {
		return model.Badge{}, fmt.Errorf("invalid data type for badge: %s", id)
	}

	return badge, nil
}

// SaveBadge saves a badge to the database
func (s *BadgeDBService) SaveBadge(ctx context.Context, badge model.Badge) error {
	ctx = appctx.WithOperationName(ctx, "save_badge")
	logger.DebugfWithContext(ctx, "Saving badge with ID: %s", badge.ID)

	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.IsRunning() {
		return fmt.Errorf("service is not running")
	}

	s.db[badge.ID] = badge

	// Schedule async backup
	go func() {
		backupCtx := appctx.NewContext(context.Background())
		backupCtx = appctx.WithServiceName(backupCtx, s.GetName())
		backupCtx = appctx.WithOperationName(backupCtx, "async_backup")

		if err := s.backupDB(backupCtx); err != nil {
			logger.WithContextError(backupCtx, err).Error("Async backup failed")
		}
	}()

	return nil
}

// Configure implements Service interface with context support
func (s *BadgeDBService) Configure(ctx context.Context, config interface{}) error {
	ctx = appctx.WithOperationName(ctx, "configure_service")
	logger.InfofWithContext(ctx, "Configuring service: %s", s.GetName())

	// Handle configuration based on type
	if configStr, ok := config.(string); ok {
		s.dbPath = configStr
		logger.DebugfWithContext(ctx, "Updated DB path to: %s", s.dbPath)
		return nil
	}

	return fmt.Errorf("unsupported configuration type")
}

// GetMetrics implements Service interface with context support
func (s *BadgeDBService) GetMetrics(ctx context.Context) map[string]interface{} {
	ctx = appctx.WithOperationName(ctx, "get_metrics")
	logger.DebugfWithContext(ctx, "Getting metrics for service: %s", s.GetName())

	s.mu.Lock()
	defer s.mu.Unlock()

	return map[string]interface{}{
		"running":     s.IsRunning(),
		"db_entries":  len(s.db),
		"last_backup": s.lastBackup.Format(time.RFC3339),
	}
}

// Helper methods
func (s *BadgeDBService) loadDB(ctx context.Context) error {
	// Simulate loading from a file
	logger.DebugfWithContext(ctx, "Loading database from: %s", s.dbPath)
	// In a real implementation, load from a file or database
	return nil
}

func (s *BadgeDBService) backupDB(ctx context.Context) error {
	// Simulate backing up to a file
	logger.DebugfWithContext(ctx, "Backing up database to: %s", s.dbPath)
	// In a real implementation, save to a file or database
	s.lastBackup = time.Now()
	return nil
}

func (s *BadgeDBService) initProcessingMap() {
	// Initialize the processing method map
	s.processingMap = map[string]string{
		"badge_award":    "StandardAward",
		"badge_revoke":   "StandardRevoke",
		"badge_transfer": "SecureTransfer",
	}
}

// badgeDBAccessChecker checks database access
type badgeDBAccessChecker struct {
	service *BadgeDBService
}

// Check implements the health.Checker interface
func (c *badgeDBAccessChecker) Check(ctx context.Context) health.CheckResult {
	result := health.NewCheckResult("database-access", health.CategoryData, health.LevelCritical)

	// Get a test badge to verify DB access
	_, err := c.service.GetBadge(ctx, "test")

	if err != nil && err.Error() != "badge not found: test" {
		// If error is not just "badge not found", it's a real error
		result.SetStatus(health.StatusDown, fmt.Sprintf("Database access error: %v", err))
	} else {
		result.SetStatus(health.StatusUp, "Database is accessible")
	}

	// Add details
	c.service.mu.Lock()
	result.AddDetail("db_entries", fmt.Sprintf("%d", len(c.service.db)))
	c.service.mu.Unlock()

	result.Complete()
	return result
}

// badgeDBBackupChecker checks database backup status
type badgeDBBackupChecker struct {
	service *BadgeDBService
}

// Check implements the health.Checker interface
func (c *badgeDBBackupChecker) Check(ctx context.Context) health.CheckResult {
	result := health.NewCheckResult("database-backup", health.CategoryData, health.LevelWarning)

	c.service.mu.Lock()
	lastBackup := c.service.lastBackup
	c.service.mu.Unlock()

	// If backup has never happened
	if lastBackup.IsZero() {
		result.SetStatus(health.StatusDegraded, "No database backup has been performed")
		result.Complete()
		return result
	}

	// Check how long since last backup
	backupAge := time.Since(lastBackup)
	result.AddDetail("last_backup", lastBackup.Format(time.RFC3339))
	result.AddDetail("backup_age_minutes", fmt.Sprintf("%.2f", backupAge.Minutes()))

	// Thresholds: Warning after 30 minutes, critical after 120 minutes
	if backupAge > 120*time.Minute {
		result.SetStatus(health.StatusDegraded, fmt.Sprintf("Database backup is too old: %.2f minutes", backupAge.Minutes()))
	} else if backupAge > 30*time.Minute {
		result.SetStatus(health.StatusDegraded, fmt.Sprintf("Database backup is getting old: %.2f minutes", backupAge.Minutes()))
	} else {
		result.SetStatus(health.StatusUp, fmt.Sprintf("Database backup is recent: %.2f minutes ago", backupAge.Minutes()))
	}

	result.Complete()
	return result
}
