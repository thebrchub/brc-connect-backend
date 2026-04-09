package config

import (
	"time"

	"github.com/shivanand-burli/go-starter-kit/helper"
)

type Config struct {
	Port      string
	Host      string
	DBUrl     string
	DBTimeout int

	RedisName string
	RedisHost string
	RedisPort int

	AdminUser string
	AdminPass string

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
	DrainBatchSize     int
	ExportMaxRows      int
	MemoryLimitMB      int
	ShutdownTimeout    int
	DailyCampaignLimit int
}

func Load() Config {
	return Config{
		Port:      helper.GetEnv("PORT", "8080"),
		Host:      helper.GetEnv("HOST", "0.0.0.0"),
		DBUrl:     helper.GetEnv("DATABASE_URL", ""),
		DBTimeout: helper.GetEnvInt("DB_TIMEOUT", 5),

		RedisName: helper.GetEnv("REDIS_NAME", "sales"),
		RedisHost: helper.GetEnv("REDIS_HOST", "localhost"),
		RedisPort: helper.GetEnvInt("REDIS_PORT", 6379),

		AdminUser: helper.GetEnv("ADMIN_USER", "admin"),
		AdminPass: helper.GetEnv("ADMIN_PASS", ""),

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
		DrainBatchSize:     helper.GetEnvInt("DRAIN_BATCH_SIZE", 100),
		ExportMaxRows:      helper.GetEnvInt("EXPORT_MAX_ROWS", 10000),
		MemoryLimitMB:      helper.GetEnvInt("MEMORY_LIMIT_MB", 256),
		ShutdownTimeout:    helper.GetEnvInt("SHUTDOWN_TIMEOUT_SEC", 15),
		DailyCampaignLimit: helper.GetEnvInt("DAILY_CAMPAIGN_LIMIT", 5),
	}
}
