package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"SigTrap-backend/internal/model"
)

// Store provides thread-safe persistence for crash events, aggregated issues, and AI diagnostics.
type Store struct {
	events        map[string]model.TrapEvent
	eventsByIssue map[string][]model.TrapEvent
	issues        map[string]*model.Issue
	diagnostics   map[string]model.AIAnalysis
	dataDir       string
	mu            sync.RWMutex
}

type storeState struct {
	Events      map[string]model.TrapEvent   `json:"events"`
	Issues      map[string]*model.Issue      `json:"issues"`
	Diagnostics map[string]model.AIAnalysis  `json:"diagnostics"`
}

// NewStore creates and initializes an in-memory telemetry store with file persistence support.
func NewStore(dataDir string) (*Store, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create store directory: %w", err)
	}

	s := &Store{
		events:        make(map[string]model.TrapEvent),
		eventsByIssue: make(map[string][]model.TrapEvent),
		issues:        make(map[string]*model.Issue),
		diagnostics:   make(map[string]model.AIAnalysis),
		dataDir:       dataDir,
	}

	_ = s.loadState()
	return s, nil
}

func (s *Store) stateFilePath() string {
	return filepath.Join(s.dataDir, "state.json")
}

func (s *Store) loadState() error {
	filePath := s.stateFilePath()
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}

	var state storeState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if state.Events != nil {
		s.events = state.Events
	}
	if state.Issues != nil {
		s.issues = state.Issues
		for _, issue := range s.issues {
			if issue.AffectedUserIDs == nil {
				issue.AffectedUserIDs = make(map[string]struct{})
			}
		}
	}
	if state.Diagnostics != nil {
		s.diagnostics = state.Diagnostics
	}

	// Rebuild eventsByIssue lookup table
	for _, event := range s.events {
		fingerprint := s.computeFingerprint(&event)
		issueID := "grp_" + fingerprint[:10]
		s.eventsByIssue[issueID] = append(s.eventsByIssue[issueID], event)
	}

	return nil
}

func (s *Store) SaveState() error {
	s.mu.RLock()
	state := storeState{
		Events:      s.events,
		Issues:      s.issues,
		Diagnostics: s.diagnostics,
	}
	data, err := json.MarshalIndent(state, "", "  ")
	s.mu.RUnlock()

	if err != nil {
		return err
	}
	return os.WriteFile(s.stateFilePath(), data, 0644)
}

func (s *Store) computeFingerprint(event *model.TrapEvent) string {
	culprit := "unknown"
	if len(event.Exception.Stacktrace) > 0 {
		culprit = event.Exception.Stacktrace[0].Filename
	}

	raw := fmt.Sprintf("%s:%s:%s", event.Exception.Type, event.Exception.Value, culprit)
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}

func (s *Store) getCulpritFile(event *model.TrapEvent) string {
	if len(event.Exception.Stacktrace) > 0 {
		frame := event.Exception.Stacktrace[0]
		return frame.Filename
	}
	return "unknown"
}

// SaveEvent processes a raw crash event, groups it into an aggregated Issue, and updates stats.
func (s *Store) SaveEvent(event model.TrapEvent) (*model.Issue, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.events[event.EventID] = event

	fingerprint := s.computeFingerprint(&event)
	issueID := "grp_" + fingerprint[:10]

	s.eventsByIssue[issueID] = append(s.eventsByIssue[issueID], event)

	culpritFile := s.getCulpritFile(&event)

	issue, exists := s.issues[issueID]
	isNewIssue := !exists

	userKey := event.User.ID
	if userKey == "" {
		userKey = event.Context.URL + ":" + event.Context.Browser
	}

	if isNewIssue {
		issue = &model.Issue{
			IssueID:         issueID,
			Type:            event.Exception.Type,
			Value:           event.Exception.Value,
			CulpritFile:     culpritFile,
			EventCount:      1,
			UsersAffected:   1,
			LastSeen:        event.Timestamp,
			FirstSeen:       event.Timestamp,
			Status:          "unresolved",
			ReleaseVersion:  event.ReleaseVersion,
			ProjectID:       event.ProjectKey,
			Environment:     event.Environment,
			AffectedUserIDs: map[string]struct{}{userKey: {}},
		}
		s.issues[issueID] = issue
	} else {
		issue.EventCount++
		if issue.AffectedUserIDs == nil {
			issue.AffectedUserIDs = make(map[string]struct{})
		}
		issue.AffectedUserIDs[userKey] = struct{}{}
		issue.UsersAffected = len(issue.AffectedUserIDs)

		if event.Timestamp > issue.LastSeen {
			issue.LastSeen = event.Timestamp
			issue.ReleaseVersion = event.ReleaseVersion
		}
	}

	return issue, isNewIssue
}

// GetIssue retrieves an aggregated issue by ID.
func (s *Store) GetIssue(issueID string) (*model.Issue, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	issue, exists := s.issues[issueID]
	if !exists {
		return nil, errors.New("issue not found")
	}
	return issue, nil
}

// UpdateIssueStatus mutates an issue's status ("unresolved", "resolved", "ignored").
func (s *Store) UpdateIssueStatus(issueID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	issue, exists := s.issues[issueID]
	if !exists {
		return errors.New("issue not found")
	}

	validStatuses := map[string]bool{"unresolved": true, "resolved": true, "ignored": true}
	if !validStatuses[status] {
		return fmt.Errorf("invalid status '%s': must be unresolved, resolved, or ignored", status)
	}

	issue.Status = status
	return nil
}

// ListIssues filters and returns aggregated issues.
func (s *Store) ListIssues(projectID, env, status, search string, limit, offset int) ([]model.Issue, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []model.Issue
	for _, issue := range s.issues {
		// Filter by ProjectID if provided
		if projectID != "" && issue.ProjectID != "" && issue.ProjectID != projectID {
			continue
		}
		// Filter by Environment
		if env != "" && env != "all" && !strings.EqualFold(issue.Environment, env) {
			continue
		}
		// Filter by Status
		if status != "" && status != "all" && !strings.EqualFold(issue.Status, status) {
			continue
		}
		// Filter by Search text
		if search != "" {
			term := strings.ToLower(search)
			matches := strings.Contains(strings.ToLower(issue.Type), term) ||
				strings.Contains(strings.ToLower(issue.Value), term) ||
				strings.Contains(strings.ToLower(issue.CulpritFile), term)
			if !matches {
				continue
			}
		}
		result = append(result, *issue)
	}

	// Sort issues descending by LastSeen
	sort.Slice(result, func(i, j int) bool {
		return result[i].LastSeen > result[j].LastSeen
	})

	total := len(result)
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []model.Issue{}, total
	}

	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}

	return result[offset:end], total
}

// GetLatestEvent returns the most recent crash event associated with an issue.
func (s *Store) GetLatestEvent(issueID string) (*model.TrapEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events, exists := s.eventsByIssue[issueID]
	if !exists || len(events) == 0 {
		return nil, errors.New("no events found for issue")
	}

	// Return event with highest timestamp
	latest := &events[0]
	for i := 1; i < len(events); i++ {
		if events[i].Timestamp > latest.Timestamp {
			latest = &events[i]
		}
	}
	return latest, nil
}

// SaveDiagnostic saves AI analysis for an issue.
func (s *Store) SaveDiagnostic(issueID string, diag model.AIAnalysis) {
	s.mu.Lock()
	defer s.mu.Unlock()
	diag.GeneratedAt = time.Now().UnixMilli()
	s.diagnostics[issueID] = diag
}

// GetDiagnostic retrieves saved AI analysis for an issue.
func (s *Store) GetDiagnostic(issueID string) (model.AIAnalysis, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	diag, ok := s.diagnostics[issueID]
	return diag, ok
}

// GetMetrics returns operation summary counters.
func (s *Store) GetMetrics() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return map[string]interface{}{
		"total_events": len(s.events),
		"total_issues": len(s.issues),
		"diagnostics":  len(s.diagnostics),
	}
}
