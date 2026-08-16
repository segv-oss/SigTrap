package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"SigTrap-backend/internal/ai"
	"SigTrap-backend/internal/config"
	"SigTrap-backend/internal/handler"
	"SigTrap-backend/internal/ingest"
	"SigTrap-backend/internal/middleware"
	"SigTrap-backend/internal/sourcemap"
	"SigTrap-backend/internal/store"
)

func main() {
	log.Println("⚡ SIGTRAP Telemetry & Post-Mortem Debugging Backend")
	log.Println("Catch the signal. Replay the state. Patch the root cause.")

	// 1. Load Configuration
	cfg := config.LoadConfig()

	// 2. Initialize Data Store
	st, err := store.NewStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("Failed to initialize store: %v", err)
	}

	// 3. Initialize SourceMap Manager
	smMgr, err := sourcemap.NewManager(cfg.DataDir)
	if err != nil {
		log.Fatalf("Failed to initialize source map manager: %v", err)
	}

	// 4. Initialize AI Diagnostic Engine
	aiEng := ai.NewEngine(cfg.GeminiAPIKey)

	// 5. Initialize Ingestion Pipeline & Worker Pool
	pipe := ingest.NewPipeline(cfg, st, smMgr, aiEng)

	// 6. Initialize HTTP Handlers & Middleware
	h := handler.NewHandler(cfg, st, smMgr, pipe, aiEng)
	limiter := middleware.NewProjectKeyLimiter(cfg.RateLimitRPS, 200)

	mux := http.NewServeMux()

	// Public Telemetry Ingestion Endpoint
	mux.Handle("/api/v1/trap", middleware.CORS(middleware.Logger(limiter.RateLimit(http.HandlerFunc(h.TrapHandler)))))

	// Admin Artifacts / Source Map Endpoints
	adminAuth := middleware.RequireAdminToken(cfg)
	mux.Handle("/api/v1/artifacts/sourcemaps", middleware.CORS(middleware.Logger(adminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.ListSourceMapsHandler(w, r)
		} else {
			h.UploadSourceMapHandler(w, r)
		}
	})))))

	// Cockpit Dashboard Issue Management Endpoints
	mux.Handle("/api/v1/issues", middleware.CORS(middleware.Logger(http.HandlerFunc(h.ListIssuesHandler))))
	mux.Handle("/api/v1/issues/", middleware.CORS(middleware.Logger(http.HandlerFunc(h.IssueRouter))))

	// System Health Endpoint
	mux.Handle("/api/v1/health", middleware.CORS(middleware.Logger(http.HandlerFunc(h.HealthHandler))))

	// 7. Start HTTP Server
	serverAddr := ":" + cfg.Port
	srv := &http.Server{
		Addr:         serverAddr,
		Handler:      middleware.Recovery(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("🚀 SigTrap Backend listening on http://localhost:%s", cfg.Port)
	log.Printf("   ├─ Ingestion Node: POST http://localhost:%s/api/v1/trap", cfg.Port)
	log.Printf("   ├─ SourceMaps:     POST http://localhost:%s/api/v1/artifacts/sourcemaps", cfg.Port)
	log.Printf("   ├─ Cockpit API:    GET  http://localhost:%s/api/v1/issues", cfg.Port)
	log.Printf("   └─ Health Check:   GET  http://localhost:%s/api/v1/health", cfg.Port)

	// Channel to listen for interrupt signals for graceful shutdown
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	<-stopChan
	log.Println("🛑 Shutdown signal received, gracefully terminating SigTrap server...")

	// Create shutdown context with 10s timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Shutdown pipeline worker pool (flushes pending channel events)
	pipe.Stop()

	// Save store state to disk
	_ = st.SaveState()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced shutdown error: %v", err)
	}

	log.Println("✅ SigTrap server stopped cleanly.")
}
