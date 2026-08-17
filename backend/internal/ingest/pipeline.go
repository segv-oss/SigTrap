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

type Pipeline struct {
	eventsChan      chan model.TrapEvent
	store           *store.Store
	sourcemapMgr    *sourcemap.Manager
	aiEngine        ai.Engine
	config          *config.Config
	workerWg        sync.WaitGroup
	eventsProcessed uint64
	eventsDropped   uint64
	cancelFunc      context.CancelFunc
}

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

	for i := 0; i < cfg.WorkerCount; i++ {
		p.workerWg.Add(1)
		go p.worker(ctx, i)
	}

	return p
}

func (p *Pipeline) Enqueue(event model.TrapEvent) bool {
	event.EnforceRingBuffer(p.config.MaxBreadcrumbs)

	select {
	case p.eventsChan <- event:
		return true
	default:
		atomic.AddUint64(&p.eventsDropped, 1)
		log.Printf("ingest buffer full (%d), dropping event_id=%s", p.config.BufferSize, event.EventID)
		return false
	}
}

func (p *Pipeline) worker(ctx context.Context, workerID int) {
	defer p.workerWg.Done()

	for {
		select {
		case <-ctx.Done():
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

	issue, _ := p.store.SaveEvent(event)
	unminifiedFrames := p.sourcemapMgr.UnminifyStacktrace(event.ReleaseVersion, event.Exception.Stacktrace)

	diag, err := p.aiEngine.Analyze(&event, unminifiedFrames)
	if err == nil {
		p.store.SaveDiagnostic(issue.IssueID, diag)
	} else {
		log.Printf("ai diagnostic failed for issue %s: %v", issue.IssueID, err)
	}

	_ = p.store.SaveState()
}

func (p *Pipeline) QueueDepth() int {
	return len(p.eventsChan)
}

func (p *Pipeline) Stats() map[string]uint64 {
	return map[string]uint64{
		"processed": atomic.LoadUint64(&p.eventsProcessed),
		"dropped":   atomic.LoadUint64(&p.eventsDropped),
	}
}

func (p *Pipeline) Stop() {
	p.cancelFunc()
	close(p.eventsChan)
	p.workerWg.Wait()
}
