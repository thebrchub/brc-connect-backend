package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/shivanand-burli/go-starter-kit/cron"
	"github.com/shivanand-burli/go-starter-kit/helper"
	"github.com/shivanand-burli/go-starter-kit/jwt"
	"github.com/shivanand-burli/go-starter-kit/postgress"
	"github.com/shivanand-burli/go-starter-kit/redis"

	"brc-connect-backend/api/config"
	apicron "brc-connect-backend/api/cron"
	"brc-connect-backend/api/handler"
	"brc-connect-backend/api/repository"
	"brc-connect-backend/api/router"
	"brc-connect-backend/api/service"
)

//go:embed database/migrations/*.sql
var migrationFS embed.FS

// logWriter prepends a log4j2-style timestamp to every log line.
type logWriter struct{}

func (lw *logWriter) Write(p []byte) (int, error) {
	ts := time.Now().Format("2006-01-02 15:04:05")
	return fmt.Fprintf(os.Stderr, "%s %s", ts, p)
}

// fatal logs and exits with a brief delay so Railway captures the output.
func fatal(format string, args ...any) {
	log.Printf(format, args...)
	time.Sleep(500 * time.Millisecond)
	os.Exit(1)
}

func main() {
	fmt.Fprintln(os.Stderr, "=== API STARTING ===")
	log.SetFlags(0)
	log.SetOutput(&logWriter{})

	// slog at WARN level — middleware.Logger only outputs 4xx/5xx (failure-only)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})))

	cfg := config.Load()
	ctx := context.Background()
	fmt.Fprintln(os.Stderr, "=== CONFIG LOADED ===")

	if cfg.SuperAdminPass == "" {
		fatal("ERROR [api] - SUPER_ADMIN_PASS must be set")
	}

	helper.TuneMemory(cfg.MemoryLimitMB)
	fmt.Fprintln(os.Stderr, "=== MEMORY TUNED ===")

	// Init Postgres
	if err := postgress.Init(cfg.DBUrl, cfg.DBTimeout); err != nil {
		fatal("ERROR [api] - postgres init failed error=%s", err)
	}
	fmt.Fprintln(os.Stderr, "=== POSTGRES OK ===")

	if helper.GetEnvBool("SKIP_MIGRATIONS", false) {
		fmt.Fprintln(os.Stderr, "=== MIGRATIONS SKIPPED ===")
	} else {
		if err := postgress.MigrateFS(ctx, migrationFS, "database/migrations"); err != nil {
			fatal("ERROR [api] - migration failed error=%s", err)
		}
		fmt.Fprintln(os.Stderr, "=== MIGRATIONS OK ===")
	}

	// Init Redis
	if err := redis.InitCache(cfg.RedisName, cfg.RedisHost, cfg.RedisPort); err != nil {
		fatal("ERROR [api] - redis init failed error=%s", err)
	}
	fmt.Fprintln(os.Stderr, "=== REDIS OK ===")

	// Init JWT
	if err := jwt.Init(); err != nil {
		fatal("ERROR [api] - jwt init failed error=%s", err)
	}

	// Repositories
	leadRepo := repository.NewLeadRepo(cfg.CacheLeadTTL, cfg.CacheFilterTTL)
	campaignRepo := repository.NewCampaignRepo(cfg.CacheCampaignTTL, cfg.CacheFilterTTL)
	jobRepo := repository.NewJobRepo()
	userRepo := repository.NewUserRepo(cfg.CacheUserTTL, cfg.CacheFilterTTL)
	activityRepo := repository.NewActivityRepo(cfg.CacheActivityTTL, cfg.CacheFilterTTL)
	sessionRepo := repository.NewSessionRepo()

	// Services
	leadSvc := service.NewLeadService(leadRepo, campaignRepo)
	campaignSvc := service.NewCampaignService(campaignRepo, jobRepo, cfg)
	userSvc := service.NewUserService(userRepo)
	activitySvc := service.NewActivityService(activityRepo, campaignRepo)
	sessionSvc := service.NewSessionService(sessionRepo)

	// Seed super admin
	if err := userSvc.SeedSuperAdmin(ctx, cfg.SuperAdminEmail, cfg.SuperAdminPass); err != nil {
		fatal("ERROR [api] - seed super admin failed error=%s", err)
	}
	fmt.Fprintln(os.Stderr, "=== SUPER ADMIN SEEDED ===")

	// Handlers
	authH := handler.NewAuthHandler(userSvc)
	leadH := handler.NewLeadHandler(leadRepo)
	campaignH := handler.NewCampaignHandler(campaignSvc)
	exportH := handler.NewExportHandler(leadRepo, cfg.ExportMaxRows)
	progressH := handler.NewProgressHandler()
	userH := handler.NewUserHandler(userSvc)
	crmH := handler.NewCRMHandler(activitySvc, sessionSvc, userSvc)

	// Router
	mux, limiter := router.New(cfg, authH, leadH, campaignH, exportH, progressH, userH, crmH)

	// Cron scheduler
	watchdog := apicron.NewWatchdog(jobRepo, cfg)
	emailVal := apicron.NewEmailValidator()
	leadRecovery := apicron.NewLeadRecovery(leadSvc, jobRepo, campaignRepo, cfg.DrainBatchSize)

	scheduler := cron.NewScheduler(cron.Config{})
	scheduler.Register("watchdog", time.Duration(cfg.WatchdogIntervalSec)*time.Second, func(ctx context.Context) {
		watchdog.Run(ctx)
	})
	scheduler.Register("email_validator", 5*time.Minute, func(ctx context.Context) {
		emailVal.Run(ctx)
	})
	scheduler.Register("lead_recovery", 30*time.Second, func(ctx context.Context) {
		leadRecovery.Run(ctx)
	})
	scheduler.Register("session_flush", 5*time.Minute, func(ctx context.Context) {
		if err := sessionSvc.FlushSessions(ctx); err != nil {
			log.Printf("ERROR [cron] - session flush failed error=%s", err)
		}
	})
	scheduler.Start()

	// HTTP server
	srv := &http.Server{
		Addr:    cfg.Host + ":" + cfg.Port,
		Handler: mux,
	}

	log.Printf("INFO  [api] - starting server addr=%s", srv.Addr)
	helper.ListenAndServe(srv, time.Duration(cfg.ShutdownTimeout)*time.Second, func() {
		scheduler.Stop()
		limiter.Close()
		postgress.Close()
		redis.Close()
	})
}
