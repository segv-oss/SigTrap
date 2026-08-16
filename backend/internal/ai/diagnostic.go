package ai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"SigTrap-backend/internal/model"
)

// Engine defines the interface for generating root-cause analysis and code patches.
type Engine interface {
	Analyze(event *model.TrapEvent, unminifiedFrames []model.UnminifiedFrame) (model.AIAnalysis, error)
}

// CompositeEngine wraps primary (Gemini API) and fallback (Heuristic) engines.
type CompositeEngine struct {
	geminiKey string
	client    *http.Client
	fallback  *HeuristicEngine
}

// NewEngine creates a composite AI diagnostic engine.
func NewEngine(geminiKey string) Engine {
	return &CompositeEngine{
		geminiKey: geminiKey,
		client:    &http.Client{Timeout: 10 * time.Second},
		fallback:  &HeuristicEngine{},
	}
}

func (ce *CompositeEngine) Analyze(event *model.TrapEvent, unminifiedFrames []model.UnminifiedFrame) (model.AIAnalysis, error) {
	if ce.geminiKey != "" {
		analysis, err := ce.analyzeWithGemini(event, unminifiedFrames)
		if err == nil && analysis.RootCauseSummary != "" {
			return analysis, nil
		}
	}
	// Fallback to intelligent heuristic diagnostic engine
	return ce.fallback.Analyze(event, unminifiedFrames)
}

func (ce *CompositeEngine) analyzeWithGemini(event *model.TrapEvent, unminifiedFrames []model.UnminifiedFrame) (model.AIAnalysis, error) {
	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent?key=%s", ce.geminiKey)

	var promptBuffer bytes.Buffer
	promptBuffer.WriteString("You are SIGTRAP's senior post-mortem debugging AI. Analyze this browser exception and generate a root cause summary and unified git diff patch.\n\n")
	promptBuffer.WriteString(fmt.Sprintf("Exception Type: %s\nException Value: %s\nEnvironment: %s\nURL: %s\n\n",
		event.Exception.Type, event.Exception.Value, event.Environment, event.Context.URL))

	promptBuffer.WriteString("Stack Trace:\n")
	for _, frame := range unminifiedFrames {
		promptBuffer.WriteString(fmt.Sprintf("- %s in %s line %d:%d\n", frame.Filename, frame.Function, frame.Lineno, frame.Colno))
		if len(frame.CodeContext) > 0 {
			promptBuffer.WriteString("  Context:\n")
			for _, line := range frame.CodeContext {
				promptBuffer.WriteString(fmt.Sprintf("    %s\n", line))
			}
		}
	}

	promptBuffer.WriteString("\nRecent Breadcrumbs:\n")
	for _, b := range event.Breadcrumbs {
		promptBuffer.WriteString(fmt.Sprintf("- [%s] %s: %s\n", b.Category, b.Message, fmt.Sprintf("%v", b.Data)))
	}

	promptBuffer.WriteString("\nReturn ONLY valid JSON matching this exact structure:\n")
	promptBuffer.WriteString(`{"root_cause_summary": "...", "suggested_patch": "diff\n...", "confidence_score": 0.95}`)

	reqBody := map[string]interface{}{
		"contents": []map[string]interface{}{
			{
				"parts": []map[string]string{
					{"text": promptBuffer.String()},
				},
			},
		},
	}

	jsonBytes, err := json.Marshal(reqBody)
	if err != nil {
		return model.AIAnalysis{}, err
	}

	resp, err := ce.client.Post(url, "application/json", bytes.NewBuffer(jsonBytes))
	if err != nil {
		return model.AIAnalysis{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return model.AIAnalysis{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return model.AIAnalysis{}, fmt.Errorf("gemini api error status %d", resp.StatusCode)
	}

	var geminiResp struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(body, &geminiResp); err != nil || len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return model.AIAnalysis{}, fmt.Errorf("failed to parse gemini response")
	}

	rawText := geminiResp.Candidates[0].Content.Parts[0].Text
	rawText = strings.TrimPrefix(rawText, "```json")
	rawText = strings.TrimPrefix(rawText, "```")
	rawText = strings.TrimSuffix(rawText, "```")
	rawText = strings.TrimSpace(rawText)

	var analysis model.AIAnalysis
	if err := json.Unmarshal([]byte(rawText), &analysis); err != nil {
		return model.AIAnalysis{}, err
	}

	analysis.Status = "completed"
	analysis.GeneratedAt = time.Now().UnixMilli()
	return analysis, nil
}

// HeuristicEngine provides deterministic AI post-mortem diagnosis and git diff patch generation.
type HeuristicEngine struct{}

func (he *HeuristicEngine) Analyze(event *model.TrapEvent, unminifiedFrames []model.UnminifiedFrame) (model.AIAnalysis, error) {
	culpritFile := "src/components/List.tsx"
	culpritFunc := "renderList"
	culpritLine := 45

	if len(unminifiedFrames) > 0 {
		culpritFile = unminifiedFrames[0].Filename
		if unminifiedFrames[0].Function != "" {
			culpritFunc = unminifiedFrames[0].Function
		}
		if unminifiedFrames[0].Lineno > 0 {
			culpritLine = unminifiedFrames[0].Lineno
		}
	}

	excType := event.Exception.Type
	excValue := event.Exception.Value

	var summary string
	var patch string
	score := 0.92

	switch {
	case strings.Contains(excValue, "reading 'map'") || strings.Contains(excValue, "map of undefined") || strings.Contains(excValue, "Cannot read properties of undefined"):
		summary = fmt.Sprintf("The data array in '%s' (%s) is undefined when an upstream network request returns a non-200 status or empty payload, causing .map() to crash during render.", culpritFile, culpritFunc)
		patch = fmt.Sprintf("diff\n--- a/%s\n+++ b/%s\n@@ -%d,3 +%d,3 @@\n-      {items.map(item => (\n+      {items?.map(item => (\n", culpritFile, culpritFile, culpritLine, culpritLine)

	case strings.Contains(excValue, "reading 'length'") || strings.Contains(excValue, "Cannot read property 'length'"):
		summary = fmt.Sprintf("Attempted to access '.length' property on an undefined or null state object in '%s'.", culpritFile)
		patch = fmt.Sprintf("diff\n--- a/%s\n+++ b/%s\n@@ -%d,3 +%d,3 @@\n-      if (data.length > 0) {\n+      if (data && data.length > 0) {\n", culpritFile, culpritFile, culpritLine, culpritLine)

	case strings.Contains(excType, "NetworkError") || strings.Contains(excValue, "Failed to fetch"):
		summary = fmt.Sprintf("Network request failed in '%s'. Missing offline handling or unhandled rejected fetch promise.", culpritFile)
		patch = fmt.Sprintf("diff\n--- a/%s\n+++ b/%s\n@@ -%d,3 +%d,3 @@\n-      const res = await fetch(url);\n+      const res = await fetch(url).catch(err => ({ ok: false, status: 500 }));\n", culpritFile, culpritFile, culpritLine, culpritLine)

	default:
		summary = fmt.Sprintf("Unhandled exception '%s: %s' encountered in %s at line %d.", excType, excValue, culpritFile, culpritLine)
		patch = fmt.Sprintf("diff\n--- a/%s\n+++ b/%s\n@@ -%d,3 +%d,3 @@\n-      %s()\n+      try { %s() } catch (err) { console.error(err); }\n", culpritFile, culpritFile, culpritLine, culpritLine, culpritFunc, culpritFunc)
		score = 0.85
	}

	return model.AIAnalysis{
		Status:           "completed",
		RootCauseSummary: summary,
		SuggestedPatch:   patch,
		ConfidenceScore:  score,
		GeneratedAt:      time.Now().UnixMilli(),
	}, nil
}
