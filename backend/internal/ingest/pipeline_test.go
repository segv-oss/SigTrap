package ingest

import (
	"os"
	"testing"
	"time"

	"SigTrap-backend/internal/ai"
	"SigTrap-backend/internal/config"
	"SigTrap-backend/internal/model"
	"SigTrap-backend/internal/sourcemap"
	"SigTrap-backend/internal/store"
)

func TestPipelineIngestionAndRingBuffer(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sigtrap_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		Port:           "8080",
		AdminToken:     "test_token",
		DataDir:        tempDir,
		MaxBreadcrumbs: 50,
		WorkerCount:    2,
		BufferSize:     100,
		RateLimitRPS:   100,
	}

	st, _ := store.NewStore(tempDir)
	smMgr, _ := sourcemap.NewManager(tempDir)
	aiEng := ai.NewEngine("")
	pipe := NewPipeline(cfg, st, smMgr, aiEng)
	defer pipe.Stop()

	// 1. Create a trap event with 60 breadcrumbs (exceeding 50 limit)
	breadcrumbs := make([]model.Breadcrumb, 60)
	for i := 0; i < 60; i++ {
		breadcrumbs[i] = model.Breadcrumb{
			Timestamp: int64(1000 + i),
			Category:  "ui.click",
			Message:   "click item",
		}
	}

	event := model.TrapEvent{
		EventID:        "123e4567-e89b-12d3-a456-426614174001",
		Timestamp:      time.Now().UnixMilli(),
		ReleaseVersion: "v1.0.0",
		Environment:    "production",
		Exception: model.Exception{
			Type:  "TypeError",
			Value: "Cannot read properties of undefined (reading 'map')",
			Stacktrace: []model.StackFrame{
				{Filename: "https://segv.tech/main.js", Lineno: 1, Colno: 100},
			},
		},
		Breadcrumbs: breadcrumbs,
		Context: model.Context{
			Browser: "Chrome 115",
			OS:      "Linux",
			URL:     "https://segv.tech/list",
			Viewport: model.Viewport{
				Width:  1920,
				Height: 1080,
			},
		},
	}

	// Test validation
	if err := event.Validate(); err != nil {
		t.Fatalf("event validation failed: %v", err)
	}

	// Enqueue event
	enqueued := pipe.Enqueue(event)
	if !enqueued {
		t.Fatalf("failed to enqueue event")
	}

	// Allow async workers time to process
	time.Sleep(200 * time.Millisecond)

	// Verify issue was aggregated in store
	issues, total := st.ListIssues("", "production", "unresolved", "", 10, 0)
	if total != 1 || len(issues) != 1 {
		t.Fatalf("expected 1 aggregated issue, got total=%d", total)
	}

	if issues[0].Type != "TypeError" {
		t.Errorf("expected issue type TypeError, got %s", issues[0].Type)
	}

	// Verify diagnostic was generated
	diag, ok := st.GetDiagnostic(issues[0].IssueID)
	if !ok {
		t.Errorf("expected AI diagnostic to be generated")
	} else if diag.Status != "completed" {
		t.Errorf("expected diagnostic status completed, got %s", diag.Status)
	}

	// Verify latest event breadcrumbs were trimmed to ring buffer limit (50)
	latest, err := st.GetLatestEvent(issues[0].IssueID)
	if err != nil {
		t.Fatalf("failed to fetch latest event: %v", err)
	}
	if len(latest.Breadcrumbs) != 50 {
		t.Errorf("expected 50 breadcrumbs after ring buffer trim, got %d", len(latest.Breadcrumbs))
	}
}
