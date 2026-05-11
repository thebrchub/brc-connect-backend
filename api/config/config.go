package config

import (
	"time"

	"github.com/shivanand-burli/go-starter-kit/helper"
)

type Config struct {
	Port string
	Host string

	SuperAdminEmail string
	SuperAdminPass  string

	WatchdogIntervalSec       int
	WatchdogStaleThresholdSec int
	WatchdogMaxAttempts       int

	CORSOrigin     string
	RateLimitRPS   int
	RateLimitBurst int

	CBFailureThreshold int
	CBOpenDurationSec  int

	CacheLeadTTL       time.Duration
	CacheCampaignTTL   time.Duration
	CacheFilterTTL     time.Duration
	CacheUserTTL       time.Duration
	CacheActivityTTL   time.Duration
	DrainBatchSize     int
	ExportMaxRows      int
	MemoryLimitMB      int
	ShutdownTimeout    int
	DailyCampaignLimit int

	// Chat (app-specific only — engine config is read by the kit)
	CacheRoomTTL         time.Duration
	MessageRetentionDays int
}

func Load() Config {
	return Config{
		Port: helper.GetEnv("PORT", "8080"),
		Host: helper.GetEnv("HOST", "0.0.0.0"),

		SuperAdminEmail: helper.GetEnv("SUPER_ADMIN_EMAIL", "admin@brc.com"),
		SuperAdminPass:  helper.GetEnv("SUPER_ADMIN_PASS", ""),

		WatchdogIntervalSec:       helper.GetEnvInt("WATCHDOG_INTERVAL_SEC", 120),
		WatchdogStaleThresholdSec: helper.GetEnvInt("WATCHDOG_STALE_THRESHOLD_SEC", 600),
		WatchdogMaxAttempts:       helper.GetEnvInt("WATCHDOG_MAX_ATTEMPTS", 3),

		CORSOrigin:     helper.GetEnv("CORS_ORIGIN", "*"),
		RateLimitRPS:   helper.GetEnvInt("RATE_LIMIT_RPS", 10),
		RateLimitBurst: helper.GetEnvInt("RATE_LIMIT_BURST", 20),

		CBFailureThreshold: helper.GetEnvInt("CB_FAILURE_THRESHOLD", 5),
		CBOpenDurationSec:  helper.GetEnvInt("CB_OPEN_DURATION_SEC", 10),

		CacheLeadTTL:       time.Duration(helper.GetEnvInt("CACHE_LEAD_TTL_SEC", 300)) * time.Second,
		CacheCampaignTTL:   time.Duration(helper.GetEnvInt("CACHE_CAMPAIGN_TTL_SEC", 60)) * time.Second,
		CacheFilterTTL:     time.Duration(helper.GetEnvInt("CACHE_FILTER_TTL_SEC", 30)) * time.Second,
		CacheUserTTL:       time.Duration(helper.GetEnvInt("CACHE_USER_TTL_SEC", 60)) * time.Second,
		CacheActivityTTL:   time.Duration(helper.GetEnvInt("CACHE_ACTIVITY_TTL_SEC", 30)) * time.Second,
		DrainBatchSize:     helper.GetEnvInt("DRAIN_BATCH_SIZE", 100),
		ExportMaxRows:      helper.GetEnvInt("EXPORT_MAX_ROWS", 10000),
		MemoryLimitMB:      helper.GetEnvInt("MEMORY_LIMIT_MB", 256),
		ShutdownTimeout:    helper.GetEnvInt("SHUTDOWN_TIMEOUT_SEC", 15),
		DailyCampaignLimit: helper.GetEnvInt("DAILY_CAMPAIGN_LIMIT", 5),

		// Chat (app-specific only)
		CacheRoomTTL:         time.Duration(helper.GetEnvInt("CACHE_ROOM_TTL_SEC", 120)) * time.Second,
		MessageRetentionDays: helper.GetEnvInt("MESSAGE_RETENTION_DAYS", 15),
	}
}
