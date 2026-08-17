package model

import (
	"errors"
	"fmt"
)

type StackFrame struct {
	Filename string `json:"filename"`
	Function string `json:"function,omitempty"`
	Lineno   int    `json:"lineno"`
	Colno    int    `json:"colno"`
}

type Exception struct {
	Type       string       `json:"type"`
	Value      string       `json:"value"`
	Stacktrace []StackFrame `json:"stacktrace"`
}

type Breadcrumb struct {
	Timestamp int64                  `json:"timestamp"`
	Category  string                 `json:"category"`
	Message   string                 `json:"message"`
	Data      map[string]interface{} `json:"data,omitempty"`
}

type Viewport struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

type Context struct {
	Browser  string   `json:"browser"`
	OS       string   `json:"os"`
	URL      string   `json:"url"`
	Viewport Viewport `json:"viewport"`
}

type UserContext struct {
	ID    string `json:"id,omitempty"`
	Email string `json:"email,omitempty"`
}

type SDKInfo struct {
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
}

type TrapEvent struct {
	EventID        string       `json:"event_id"`
	Timestamp      int64        `json:"timestamp"`
	ReleaseVersion string       `json:"release_version"`
	Environment    string       `json:"environment"`
	Exception      Exception    `json:"exception"`
	Breadcrumbs    []Breadcrumb `json:"breadcrumbs"`
	Context        Context      `json:"context"`
	User           UserContext  `json:"user,omitempty"`
	SDK            SDKInfo      `json:"sdk,omitempty"`
	ProjectKey     string       `json:"project_key,omitempty"`
}

func (e *TrapEvent) Validate() error {
	if e.EventID == "" {
		return errors.New("event_id is required")
	}
	if e.Timestamp <= 0 {
		return errors.New("timestamp must be a positive unix millisecond timestamp")
	}
	if e.ReleaseVersion == "" {
		return errors.New("release_version is required")
	}
	if e.Environment == "" {
		return errors.New("environment is required")
	}
	if e.Exception.Type == "" {
		return errors.New("exception.type is required")
	}
	if e.Exception.Value == "" {
		return errors.New("exception.value is required")
	}
	if len(e.Exception.Stacktrace) == 0 {
		return errors.New("exception.stacktrace must contain at least one stack frame")
	}
	for i, frame := range e.Exception.Stacktrace {
		if frame.Filename == "" {
			return fmt.Errorf("stacktrace frame [%d] missing filename", i)
		}
		if frame.Lineno <= 0 {
			return fmt.Errorf("stacktrace frame [%d] lineno must be > 0", i)
		}
		if frame.Colno < 0 {
			return fmt.Errorf("stacktrace frame [%d] colno must be >= 0", i)
		}
	}
	return nil
}

func (e *TrapEvent) EnforceRingBuffer(maxLimit int) {
	if maxLimit > 0 && len(e.Breadcrumbs) > maxLimit {
		startIndex := len(e.Breadcrumbs) - maxLimit
		e.Breadcrumbs = e.Breadcrumbs[startIndex:]
	}
}

type Issue struct {
	IssueID         string              `json:"issue_id"`
	Type            string              `json:"type"`
	Value           string              `json:"value"`
	CulpritFile     string              `json:"culprit_file"`
	EventCount      int                 `json:"event_count"`
	UsersAffected   int                 `json:"users_affected"`
	LastSeen        int64               `json:"last_seen"`
	FirstSeen       int64               `json:"first_seen"`
	Status          string              `json:"status"`
	ReleaseVersion  string              `json:"release_version"`
	ProjectID       string              `json:"project_id"`
	Environment     string              `json:"environment"`
	AffectedUserIDs map[string]struct{} `json:"-"`
}

type UnminifiedFrame struct {
	Filename    string   `json:"filename"`
	Function    string   `json:"function"`
	Lineno      int      `json:"lineno"`
	Colno       int      `json:"colno"`
	CodeContext []string `json:"code_context"`
}

type LatestEventDetails struct {
	EventID              string            `json:"event_id"`
	Timestamp            int64             `json:"timestamp"`
	Environment          string            `json:"environment"`
	ReleaseVersion       string            `json:"release_version"`
	UnminifiedStacktrace []UnminifiedFrame `json:"unminified_stacktrace"`
	Breadcrumbs          []Breadcrumb      `json:"breadcrumbs"`
	Context              Context           `json:"context"`
}

type AIAnalysis struct {
	Status           string  `json:"status"`
	RootCauseSummary string  `json:"root_cause_summary"`
	SuggestedPatch   string  `json:"suggested_patch"`
	ConfidenceScore  float64 `json:"confidence_score"`
	GeneratedAt      int64   `json:"generated_at"`
}

type DiagnosticResponse struct {
	IssueID     string             `json:"issue_id"`
	AIAnalysis  AIAnalysis         `json:"ai_analysis"`
	LatestEvent LatestEventDetails `json:"latest_event"`
}

type IssueListResponse struct {
	Issues []Issue `json:"issues"`
	Total  int     `json:"total"`
	Limit  int     `json:"limit"`
	Offset int     `json:"offset"`
}

type SourceMapMeta struct {
	ReleaseVersion string `json:"release_version"`
	Filename       string `json:"filename"`
	SizeBytes      int64  `json:"size_bytes"`
	UploadedAt     int64  `json:"uploaded_at"`
}
