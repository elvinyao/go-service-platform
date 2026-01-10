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
	// Other necessary imports
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

	if s.IsRunning(ctx) {
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

	if !s.IsRunning(ctx) {
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

	if !s.IsRunning(ctx) {
		logger.InfofWithContext(ctx, "Service %s is not running, starting it", s.GetName())
		return s.Start(ctx)
	}

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

	if !s.IsRunning(ctx) {
		return model.Badge{}, fmt.Errorf("service not running")
	}

	logger.DebugfWithContext(ctx, "Getting badge with ID: %s", id)

	s.mu.Lock()
	defer s.mu.Unlock()

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

	if !s.IsRunning(ctx) {
		return fmt.Errorf("service not running")
	}

	logger.DebugfWithContext(ctx, "Saving badge with ID: %s", badge.ID)

	s.mu.Lock()
	defer s.mu.Unlock()

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
		"running":     s.IsRunning(ctx),
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

// Check implements the Checker interface
func (c *badgeDBAccessChecker) Check(ctx context.Context) *health.CheckResult {
	result := health.NewCheckResult("badgedb-access", health.CategoryData)
	result.Level = health.LevelCritical

	// Check if service is running
	if !c.service.IsRunning(ctx) {
		result.SetStatus(health.StatusDown, "BadgeDB service is not running")
		result.Complete()
		return result
	}

	// Check database access
	_, err := c.service.GetBadge(ctx, "test-badge-id")
	if err != nil {
		// It's ok if the badge doesn't exist, we just want to check access
		if err.Error() != "badge not found" {
			result.SetStatus(health.StatusDegraded, fmt.Sprintf("Database access error: %v", err))
			result.Complete()
			return result
		}
	}

	result.SetStatus(health.StatusUp, "Database access is working")
	result.Complete()
	return result
}

// badgeDBBackupChecker checks database backup status
type badgeDBBackupChecker struct {
	service *BadgeDBService
}

// Check implements the Checker interface
func (c *badgeDBBackupChecker) Check(ctx context.Context) *health.CheckResult {
	result := health.NewCheckResult("badgedb-backup", health.CategoryData)
	result.Level = health.LevelWarning

	// Check if service is running
	if !c.service.IsRunning(ctx) {
		result.SetStatus(health.StatusDown, "BadgeDB service is not running")
		result.Complete()
		return result
	}

	// Check last backup time
	c.service.mu.Lock()
	lastBackup := c.service.lastBackup
	c.service.mu.Unlock()

	if lastBackup.IsZero() {
		result.SetStatus(health.StatusDegraded, "No backup has been performed")
	} else {
		backupAge := time.Since(lastBackup)
		result.AddDetail("last_backup_age_hours", backupAge.Hours())

		if backupAge > 24*time.Hour {
			result.SetStatus(health.StatusDegraded, fmt.Sprintf("Backup is too old: %v", backupAge))
		} else {
			result.SetStatus(health.StatusUp, fmt.Sprintf("Last backup was %v ago", backupAge))
		}
	}

	result.Complete()
	return result
}
