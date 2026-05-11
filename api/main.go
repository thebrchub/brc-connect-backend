package main

import (
	"context"
	"embed"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/shivanand-burli/go-starter-kit/chat"
	"github.com/shivanand-burli/go-starter-kit/cron"
	"github.com/shivanand-burli/go-starter-kit/helper"
	"github.com/shivanand-burli/go-starter-kit/jwt"
	"github.com/shivanand-burli/go-starter-kit/postgress"
	"github.com/shivanand-burli/go-starter-kit/redis"
	"github.com/shivanand-burli/go-starter-kit/rtc"
	"github.com/shivanand-burli/go-starter-kit/storage"

	"brc-connect-backend/api/config"
	apicron "brc-connect-backend/api/cron"
	"brc-connect-backend/api/handler"
	"brc-connect-backend/api/repository"
	"brc-connect-backend/api/router"
	"brc-connect-backend/api/service"

	"github.com/joho/godotenv"
)

//go:embed database/migrations/*.sql
var migrationFS embed.FS

// chatAllowedMIME extends the kit default (images) with docs/audio/video.
var chatAllowedMIME = map[string]bool{
	// Images (from kit default)
	"image/jpeg": true, "image/png": true, "image/gif": true, "image/webp": true,
	// Video
	"video/mp4": true, "video/webm": true,
	// Audio
	"audio/mpeg": true, "audio/ogg": true, "audio/webm": true,
	// Documents
	"application/pdf":    true,
	"application/msword": true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": true,
	"application/vnd.ms-excel": true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet": true,
	"text/plain": true,
	"text/csv":   true,
}

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
	_ = godotenv.Load() // load .env if present (no error if missing)

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
	if err := postgress.Init(); err != nil {
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
	if err := redis.Init(); err != nil {
		fatal("ERROR [api] - redis init failed error=%s", err)
	}
	fmt.Fprintln(os.Stderr, "=== REDIS OK ===")

	// Init JWT
	if err := jwt.Init(); err != nil {
		fatal("ERROR [api] - jwt init failed error=%s", err)
	}

	// Init Storage (optional — file uploads disabled if env vars missing)
	var store storage.StorageService
	if helper.GetEnv("STORAGE_BUCKET", "") != "" {
		s3c, err := storage.NewS3Client(storage.S3Config{
			AllowedMIME: chatAllowedMIME,
		})
		if err != nil {
			fatal("ERROR [api] - storage init failed error=%s", err)
		}
		store = s3c
		fmt.Fprintln(os.Stderr, "=== STORAGE OK ===")
	} else {
		fmt.Fprintln(os.Stderr, "=== STORAGE SKIPPED (no STORAGE_BUCKET) ===")
	}

	// Repositories
	leadRepo := repository.NewLeadRepo(cfg.CacheLeadTTL, cfg.CacheFilterTTL)
	campaignRepo := repository.NewCampaignRepo(cfg.CacheCampaignTTL, cfg.CacheFilterTTL)
	jobRepo := repository.NewJobRepo()
	userRepo := repository.NewUserRepo(cfg.CacheUserTTL, cfg.CacheFilterTTL)
	activityRepo := repository.NewActivityRepo(cfg.CacheActivityTTL, cfg.CacheFilterTTL)
	sessionRepo := repository.NewSessionRepo()

	// Services
	leadSvc := service.NewLeadService(leadRepo, campaignRepo, jobRepo)
	campaignSvc := service.NewCampaignService(campaignRepo, jobRepo, activityRepo, cfg)
	userSvc := service.NewUserService(userRepo)
	activitySvc := service.NewActivityService(activityRepo, campaignRepo, leadRepo)
	sessionSvc := service.NewSessionService(sessionRepo)

	// Seed super admin
	if err := userSvc.SeedSuperAdmin(ctx, cfg.SuperAdminEmail, cfg.SuperAdminPass); err != nil {
		fatal("ERROR [api] - seed super admin failed error=%s", err)
	}
	fmt.Fprintln(os.Stderr, "=== SUPER ADMIN SEEDED ===")

	// Handlers
	authH := handler.NewAuthHandler(userSvc)
	leadH := handler.NewLeadHandler(leadRepo, userRepo, activityRepo)
	campaignH := handler.NewCampaignHandler(campaignSvc)
	exportH := handler.NewExportHandler(leadRepo, cfg.ExportMaxRows)
	progressH := handler.NewProgressHandler()
	userH := handler.NewUserHandler(userSvc, store)
	crmH := handler.NewCRMHandler(activitySvc, sessionSvc, userSvc)

	// Chat
	roomRepo := repository.NewRoomRepo(cfg.CacheRoomTTL)
	msgRepo := repository.NewMessageRepo()
	callRepo := &repository.CallRepo{}
	chatSvc := service.NewChatService(roomRepo, msgRepo)
	chatH := handler.NewChatHandler(chatSvc, store)

	// LiveKit RTC client (optional — group calls disabled if env vars missing)
	rtcClient := rtc.NewClientOptional(rtc.Config{
		URL:       helper.GetEnv("LIVEKIT_URL", ""),
		APIKey:    helper.GetEnv("LIVEKIT_API_KEY", ""),
		APISecret: helper.GetEnv("LIVEKIT_API_SECRET", ""),
	})
	gcSvc := service.NewGroupCallService(roomRepo, rtcClient)
	gcH := handler.NewGroupCallHandler(gcSvc)

	// Init chat engine (config read from CHAT_* env vars by the kit)
	if err := chat.Init(chat.Config{
		GroupCallRTC:       rtcClient,
		MaxCallDurationSec: 1800, // 30 min cap
		RetentionDays:      cfg.MessageRetentionDays,
	}, chat.Hooks{
		LoadUserRooms: func(ctx context.Context, userID string) ([]string, error) {
			return chatSvc.GetAllUserRoomIDs(ctx, userID)
		},
		OnFlush: func(ctx context.Context, roomID string, messages [][]byte) error {
			return chatSvc.FlushRoomMessages(ctx, roomID, messages)
		},
		CanCall: func(ctx context.Context, callerID, calleeID string) bool {
			ok, err := chatSvc.SameOrg(ctx, callerID, calleeID)
			if err != nil {
				log.Printf("ERROR [chat] - can-call check failed from=%s to=%s error=%s", callerID, calleeID, err)
				return false
			}
			return ok
		},
		IsPresenceHidden: func(userID string) bool {
			u, err := userRepo.GetByID(context.Background(), userID)
			if err != nil || u == nil {
				return false
			}
			return u.PresenceHidden
		},
		OnFlushCallEvents: func(ctx context.Context, events []chat.CallEvent) {
			log.Printf("[chat] OnFlushCallEvents called with %d events", len(events))
			for _, event := range events {
				log.Printf("[chat] event: callID=%s status=%s type=%s roomID=%s callerID=%s", event.CallID, event.Status, event.Type, event.RoomID, event.CallerID)
				var err error
				if event.RoomID != "" {
					// Group call events
					switch event.Status {
					case chat.CallRinging:
						err = callRepo.InsertGroupCallLog(ctx, event.CallID, event.RoomID, event.CallerID)
					case chat.CallCompleted:
						err = callRepo.MarkGroupCallCompleted(ctx, event.CallID)
					}
				} else {
					// P2P call events
					switch event.Status {
					case chat.CallRinging, chat.CallBusy:
						err = callRepo.InsertCallLog(ctx, event.CallID, event.CallerID, event.CalleeID, string(event.Type), string(event.Status))
					case chat.CallAnswered:
						err = callRepo.MarkAnswered(ctx, event.CallID, event.StartedAt)
					case chat.CallCompleted:
						err = callRepo.MarkCompleted(ctx, event.CallID, event.Duration)
					case chat.CallMissed, chat.CallRejected:
						err = callRepo.MarkTerminal(ctx, event.CallID, string(event.Status))
					}
				}
				if err != nil {
					log.Printf("ERROR [chat] - persist call event failed call_id=%s status=%s error=%s", event.CallID, event.Status, err)
				}
			}
		},
		OnPurge: func(ctx context.Context, olderThan time.Time) (int64, error) {
			const batchSize = 500

			// 1. Hard-delete messages, collect media URLs
			msgCount, mediaURLs, err := msgRepo.PurgeOldMessages(ctx, olderThan, batchSize)
			if err != nil {
				return msgCount, fmt.Errorf("purge messages: %w", err)
			}

			// 2. Delete chat files from S3 (only chat/ prefix keys, not avatars)
			if store != nil && len(mediaURLs) > 0 {
				keys := make([]string, 0, len(mediaURLs))
				for _, u := range mediaURLs {
					// media_url is "{publicURL}/chat/{userId}/{uuid}{ext}" — extract key after last "/chat/"
					if idx := strings.Index(u, "/chat/"); idx >= 0 {
						keys = append(keys, u[idx+1:]) // "chat/{userId}/{uuid}{ext}"
					}
				}
				if len(keys) > 0 {
					if err := store.DeleteBatch(ctx, keys); err != nil {
						log.Printf("WARN  [chat] - purge S3 batch delete failed count=%d error=%s", len(keys), err)
						// Don't fail the whole purge — DB rows already deleted
					}
				}
			}

			// 3. Hard-delete call logs
			callCount, err := callRepo.PurgeOldCallLogs(ctx, olderThan, batchSize)
			if err != nil {
				log.Printf("WARN  [chat] - purge call logs failed error=%s", err)
				// Don't fail — messages already purged
			}

			log.Printf("[chat] purge complete messages=%d files=%d calls=%d", msgCount, len(mediaURLs), callCount)
			return msgCount + callCount, nil
		},
	}); err != nil {
		fatal("ERROR [api] - chat engine init failed error=%s", err)
	}
	fmt.Fprintln(os.Stderr, "=== CHAT ENGINE OK ===")

	// Router
	mux, limiter := router.New(cfg, authH, leadH, campaignH, exportH, progressH, userH, crmH, chatH, gcH, rtcClient)

	// Cron scheduler
	watchdog := apicron.NewWatchdog(jobRepo, cfg)
	emailVal := apicron.NewEmailValidator()
	leadRecovery := apicron.NewLeadRecovery(leadSvc, jobRepo, campaignRepo, activityRepo, cfg.DrainBatchSize)

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
		chat.Close()
		limiter.Close()
		postgress.Close()
		redis.Close()
	})
}
