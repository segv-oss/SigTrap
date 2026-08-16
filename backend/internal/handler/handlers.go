package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"SigTrap-backend/internal/ai"
	"SigTrap-backend/internal/config"
	"SigTrap-backend/internal/ingest"
	"SigTrap-backend/internal/middleware"
	"SigTrap-backend/internal/model"
	"SigTrap-backend/internal/sourcemap"
	"SigTrap-backend/internal/store"
)

// Handler holds dependencies for all API handlers.
type Handler struct {
	config       *config.Config
	store        *store.Store
	sourcemapMgr *sourcemap.Manager
	pipeline     *ingest.Pipeline
	aiEngine     ai.Engine
}

// NewHandler initializes a Handler with required services.
func NewHandler(cfg *config.Config, st *store.Store, smMgr *sourcemap.Manager, pipe *ingest.Pipeline, aiEng ai.Engine) *Handler {
	return &Handler{
		config:       cfg,
		store:        st,
		sourcemapMgr: smMgr,
		pipeline:     pipe,
		aiEngine:     aiEng,
	}
}

// TrapHandler handles POST /api/v1/trap.
// Ingests incoming client telemetry, validates payload, trims ring buffer, and queues for async processing.
func (h *Handler) TrapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.WriteJSONError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST method is allowed.")
		return
	}

	projectKey := r.Header.Get("X-SigTrap-Project-Key")
	if projectKey == "" {
		middleware.WriteJSONError(w, http.StatusUnauthorized, "MISSING_PROJECT_KEY", "Header 'X-SigTrap-Project-Key' is required.")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20)) // 1MB payload limit
	if err != nil {
		middleware.WriteJSONError(w, http.StatusBadRequest, "PAYLOAD_TOO_LARGE", "Request body exceeds maximum allowed size of 1MB.")
		return
	}

	var event model.TrapEvent
	if err := json.Unmarshal(body, &event); err != nil {
		middleware.WriteJSONError(w, http.StatusBadRequest, "INVALID_JSON", "Failed to parse JSON body: "+err.Error())
		return
	}

	event.ProjectKey = projectKey

	// Validate payload structure & types
	if err := event.Validate(); err != nil {
		middleware.WriteJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}

	// Enforce breadcrumb ring buffer cap (max 50)
	event.EnforceRingBuffer(h.config.MaxBreadcrumbs)

	// Non-blocking push into pipeline queue
	if ok := h.pipeline.Enqueue(event); !ok {
		middleware.WriteJSONError(w, http.StatusServiceUnavailable, "QUEUE_FULL", "Ingestion pipeline buffer is temporarily full.")
		return
	}

	// Contract: return 202 Accepted immediately with empty body
	w.WriteHeader(http.StatusAccepted)
}

// UploadSourceMapHandler handles POST /api/v1/artifacts/sourcemaps.
func (h *Handler) UploadSourceMapHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		middleware.WriteJSONError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only POST method is allowed.")
		return
	}

	// Parse multipart/form-data with max 32MB file limit
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		middleware.WriteJSONError(w, http.StatusBadRequest, "INVALID_MULTIPART", "Failed to parse multipart form: "+err.Error())
		return
	}

	releaseVersion := r.FormValue("release_version")
	if releaseVersion == "" {
		middleware.WriteJSONError(w, http.StatusBadRequest, "MISSING_FIELD", "Form field 'release_version' is required.")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		// Fallback check for "sourcemap" parameter name
		file, header, err = r.FormFile("sourcemap")
	}
	if err != nil {
		middleware.WriteJSONError(w, http.StatusBadRequest, "MISSING_FILE", "Multipart file upload ('file' or 'sourcemap') is required.")
		return
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		middleware.WriteJSONError(w, http.StatusInternalServerError, "FILE_READ_ERROR", "Failed to read uploaded source map file.")
		return
	}

	meta, err := h.sourcemapMgr.SaveSourceMap(releaseVersion, header.Filename, content)
	if err != nil {
		middleware.WriteJSONError(w, http.StatusInternalServerError, "STORAGE_ERROR", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":          "success",
		"message":         "Source map successfully uploaded and indexed.",
		"release_version": meta.ReleaseVersion,
		"filename":        meta.Filename,
		"size_bytes":      meta.SizeBytes,
		"uploaded_at":     meta.UploadedAt,
	})
}

// ListSourceMapsHandler handles GET /api/v1/artifacts/sourcemaps.
func (h *Handler) ListSourceMapsHandler(w http.ResponseWriter, r *http.Request) {
	relVersion := r.URL.Query().Get("release_version")
	maps := h.sourcemapMgr.ListSourceMaps(relVersion)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"sourcemaps": maps,
	})
}

// ListIssuesHandler handles GET /api/v1/issues.
func (h *Handler) ListIssuesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		middleware.WriteJSONError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET method is allowed.")
		return
	}

	projectID := r.URL.Query().Get("project_id")
	if projectID == "" {
		projectID = r.Header.Get("X-SigTrap-Project-Key")
	}

	env := r.URL.Query().Get("env")
	if env == "" {
		env = "production"
	}

	status := r.URL.Query().Get("status")
	search := r.URL.Query().Get("search")

	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	offsetStr := r.URL.Query().Get("offset")
	offset := 0
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	issues, total := h.store.ListIssues(projectID, env, status, search, limit, offset)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(model.IssueListResponse{
		Issues: issues,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

// IssueDetailOrDiagnosticHandler routes GET / PATCH on /api/v1/issues/:issue_id and /api/v1/issues/:issue_id/diagnostic.
func (h *Handler) IssueRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/issues/")
	parts := strings.Split(strings.Trim(path, "/"), "/")

	if len(parts) == 0 || parts[0] == "" {
		h.ListIssuesHandler(w, r)
		return
	}

	issueID := parts[0]

	// GET /api/v1/issues/:issue_id/diagnostic
	if len(parts) >= 2 && parts[1] == "diagnostic" {
		if len(parts) == 3 && parts[2] == "reanalyze" && r.Method == http.MethodPost {
			h.ReanalyzeDiagnosticHandler(w, r, issueID)
			return
		}
		if r.Method == http.MethodGet {
			h.GetIssueDiagnosticHandler(w, r, issueID)
			return
		}
	}

	// PATCH /api/v1/issues/:issue_id
	if r.Method == http.MethodPatch {
		h.UpdateIssueStatusHandler(w, r, issueID)
		return
	}

	// GET /api/v1/issues/:issue_id
	if r.Method == http.MethodGet {
		h.GetIssueSummaryHandler(w, r, issueID)
		return
	}

	middleware.WriteJSONError(w, http.StatusNotFound, "ROUTE_NOT_FOUND", "Endpoint not found.")
}

func (h *Handler) GetIssueSummaryHandler(w http.ResponseWriter, r *http.Request, issueID string) {
	issue, err := h.store.GetIssue(issueID)
	if err != nil {
		middleware.WriteJSONError(w, http.StatusNotFound, "ISSUE_NOT_FOUND", err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(issue)
}

func (h *Handler) UpdateIssueStatusHandler(w http.ResponseWriter, r *http.Request, issueID string) {
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		middleware.WriteJSONError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid JSON payload.")
		return
	}

	if err := h.store.UpdateIssueStatus(issueID, body.Status); err != nil {
		middleware.WriteJSONError(w, http.StatusBadRequest, "INVALID_STATUS", err.Error())
		return
	}

	_ = h.store.SaveState()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"issue_id":   issueID,
		"status":     body.Status,
		"updated_at": time.Now().UnixMilli(),
	})
}

func (h *Handler) GetIssueDiagnosticHandler(w http.ResponseWriter, r *http.Request, issueID string) {
	issue, err := h.store.GetIssue(issueID)
	if err != nil {
		middleware.WriteJSONError(w, http.StatusNotFound, "ISSUE_NOT_FOUND", "Issue "+issueID+" not found.")
		return
	}

	latestEvent, err := h.store.GetLatestEvent(issueID)
	if err != nil {
		middleware.WriteJSONError(w, http.StatusNotFound, "NO_EVENTS", "No events found for issue "+issueID)
		return
	}

	// Resolve unminified stacktrace with code context
	unminifiedFrames := h.sourcemapMgr.UnminifyStacktrace(latestEvent.ReleaseVersion, latestEvent.Exception.Stacktrace)

	// Fetch or generate AI analysis
	diag, ok := h.store.GetDiagnostic(issueID)
	if !ok {
		diag, _ = h.aiEngine.Analyze(latestEvent, unminifiedFrames)
		h.store.SaveDiagnostic(issueID, diag)
	}

	resp := model.DiagnosticResponse{
		IssueID:    issue.IssueID,
		AIAnalysis: diag,
		LatestEvent: model.LatestEventDetails{
			EventID:              latestEvent.EventID,
			Timestamp:            latestEvent.Timestamp,
			Environment:          latestEvent.Environment,
			ReleaseVersion:       latestEvent.ReleaseVersion,
			UnminifiedStacktrace: unminifiedFrames,
			Breadcrumbs:          latestEvent.Breadcrumbs,
			Context:              latestEvent.Context,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (h *Handler) ReanalyzeDiagnosticHandler(w http.ResponseWriter, r *http.Request, issueID string) {
	latestEvent, err := h.store.GetLatestEvent(issueID)
	if err != nil {
		middleware.WriteJSONError(w, http.StatusNotFound, "NO_EVENTS", "No events available to analyze for issue "+issueID)
		return
	}

	unminifiedFrames := h.sourcemapMgr.UnminifyStacktrace(latestEvent.ReleaseVersion, latestEvent.Exception.Stacktrace)
	diag, err := h.aiEngine.Analyze(latestEvent, unminifiedFrames)
	if err != nil {
		middleware.WriteJSONError(w, http.StatusInternalServerError, "AI_ERROR", err.Error())
		return
	}

	h.store.SaveDiagnostic(issueID, diag)
	_ = h.store.SaveState()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"issue_id": issueID,
		"status":   "completed",
		"message":  "AI root-cause diagnostic re-analysis completed.",
		"analysis": diag,
	})
}

// HealthHandler handles GET /api/v1/health.
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	metrics := h.store.GetMetrics()
	pipelineStats := h.pipeline.Stats()

	resp := map[string]interface{}{
		"status":    "healthy",
		"service":   "sigtrap-backend",
		"version":   "1.0.0",
		"timestamp": time.Now().UnixMilli(),
		"metrics": map[string]interface{}{
			"events_ingested":   pipelineStats["processed"],
			"events_dropped":    pipelineStats["dropped"],
			"total_issues":      metrics["total_issues"],
			"queue_depth":       h.pipeline.QueueDepth(),
			"worker_count":      h.config.WorkerCount,
			"rate_limit_rps":    h.config.RateLimitRPS,
			"max_breadcrumbs":   h.config.MaxBreadcrumbs,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
