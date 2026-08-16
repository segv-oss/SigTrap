package ingest

import (
	"context"
	"log"
	"sync"
	"sync/atomic"

	"SigTrap-backend/internal/ai"
	"SigTrap-backend/internal/config"
	"SigTrap-backend/internal/model"
	"SigTrap-backend/internal/sourcemap"
	"SigTrap-backend/internal/store"
)

// Pipeline manages high-throughput asynchronous ingestion and background event processing.
type Pipeline struct {
	eventsChan     chan model.TrapEvent
	store          *store.Store
	sourcemapMgr   *sourcemap.Manager
	aiEngine       ai.Engine
	config         *config.Config
	workerWg       sync.WaitGroup
	eventsProcessed uint64
	eventsDropped   uint64
	cancelFunc     context.CancelFunc
}

// NewPipeline initializes an async ingestion pipeline with worker pool.
func NewPipeline(cfg *config.Config, st *store.Store, smMgr *sourcemap.Manager, aiEng ai.Engine) *Pipeline {
	ctx, cancel := context.WithCancel(context.Background())

	p := &Pipeline{
		eventsChan:   make(chan model.TrapEvent, cfg.BufferSize),
		store:        st,
		sourcemapMgr: smMgr,
		aiEngine:     aiEng,
		config:       cfg,
		cancelFunc:   cancel,
	}

	// Start worker pool
	for i := 0; i < cfg.WorkerCount; i++ {
		p.workerWg.Add(1)
		go p.worker(ctx, i)
	}

	return p
}

// Enqueue puts an incoming trap event into the non-blocking channel buffer.
func (p *Pipeline) Enqueue(event model.TrapEvent) bool {
	// Enforce 50-item ring buffer constraint on breadcrumbs
	event.EnforceRingBuffer(p.config.MaxBreadcrumbs)

	select {
	case p.eventsChan <- event:
		return true
	default:
		atomic.AddUint64(&p.eventsDropped, 1)
		log.Printf("[PIPELINE WARN] Event queue buffer full (%d), dropped event_id=%s", p.config.BufferSize, event.EventID)
		return false
	}
}

func (p *Pipeline) worker(ctx context.Context, workerID int) {
	defer p.workerWg.Done()

	for {
		select {
		case <-ctx.Done():
			// Drain remaining events in channel before exit
			for len(p.eventsChan) > 0 {
				event := <-p.eventsChan
				p.processEvent(event)
			}
			return

		case event, ok := <-p.eventsChan:
			if !ok {
				return
			}
			p.processEvent(event)
		}
	}
}

func (p *Pipeline) processEvent(event model.TrapEvent) {
	atomic.AddUint64(&p.eventsProcessed, 1)

	// 1. Group event into aggregated issue in thread-safe store
	issue, _ := p.store.SaveEvent(event)

	// 2. Resolve minified stack trace into unminified frames with code context
	unminifiedFrames := p.sourcemapMgr.UnminifyStacktrace(event.ReleaseVersion, event.Exception.Stacktrace)

	// 3. Run AI root-cause diagnostic analysis
	diag, err := p.aiEngine.Analyze(&event, unminifiedFrames)
	if err == nil {
		p.store.SaveDiagnostic(issue.IssueID, diag)
	} else {
		log.Printf("[PIPELINE ERROR] AI diagnostic failed for issue %s: %v", issue.IssueID, err)
	}

	// 4. Persist store state asynchronously
	_ = p.store.SaveState()
}

// QueueDepth returns the current number of pending items in the channel.
func (p *Pipeline) QueueDepth() int {
	return len(p.eventsChan)
}

// Stats returns processing counters.
func (p *Pipeline) Stats() map[string]uint64 {
	return map[string]uint64{
		"processed": atomic.LoadUint64(&p.eventsProcessed),
		"dropped":   atomic.LoadUint64(&p.eventsDropped),
	}
}

// Stop gracefully shuts down the worker pool and flushes channel.
func (p *Pipeline) Stop() {
	p.cancelFunc()
	close(p.eventsChan)
	p.workerWg.Wait()
}
