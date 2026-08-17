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
	cfg := config.LoadConfig()

	st, err := store.NewStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("failed to initialize store: %v", err)
	}

	smMgr, err := sourcemap.NewManager(cfg.DataDir)
	if err != nil {
		log.Fatalf("failed to initialize sourcemap manager: %v", err)
	}

	aiEng := ai.NewEngine(cfg.GeminiAPIKey)
	pipe := ingest.NewPipeline(cfg, st, smMgr, aiEng)

	h := handler.NewHandler(cfg, st, smMgr, pipe, aiEng)
	limiter := middleware.NewProjectKeyLimiter(cfg.RateLimitRPS, 200)

	mux := http.NewServeMux()
	mux.Handle("/api/v1/trap", middleware.CORS(middleware.Logger(limiter.RateLimit(http.HandlerFunc(h.TrapHandler)))))

	adminAuth := middleware.RequireAdminToken(cfg)
	mux.Handle("/api/v1/artifacts/sourcemaps", middleware.CORS(middleware.Logger(adminAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.ListSourceMapsHandler(w, r)
		} else {
			h.UploadSourceMapHandler(w, r)
		}
	})))))

	mux.Handle("/api/v1/issues", middleware.CORS(middleware.Logger(http.HandlerFunc(h.ListIssuesHandler))))
	mux.Handle("/api/v1/issues/", middleware.CORS(middleware.Logger(http.HandlerFunc(h.IssueRouter))))
	mux.Handle("/api/v1/health", middleware.CORS(middleware.Logger(http.HandlerFunc(h.HealthHandler))))

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      middleware.Recovery(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Printf("Server listening on :%s", cfg.Port)

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	<-stopChan
	log.Println("shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pipe.Stop()
	_ = st.SaveState()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("forced shutdown error: %v", err)
	}

	log.Println("server stopped gracefully")
}
