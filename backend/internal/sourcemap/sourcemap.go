package sourcemap

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"SigTrap-backend/internal/model"
)

type RawSourceMap struct {
	Version        int      `json:"version"`
	File           string   `json:"file"`
	Sources        []string `json:"sources"`
	SourcesContent []string `json:"sourcesContent"`
	Names          []string `json:"names"`
	Mappings       string   `json:"mappings"`
}

type DecodedMapping struct {
	GeneratedLine   int
	GeneratedColumn int
	OriginalSource  string
	OriginalLine    int
	OriginalColumn  int
	Name            string
}

type Manager struct {
	dataDir string
	cache   map[string]*RawSourceMap
	meta    map[string][]model.SourceMapMeta
	mu      sync.RWMutex
}

func NewManager(dataDir string) (*Manager, error) {
	sourcemapDir := filepath.Join(dataDir, "sourcemaps")
	if err := os.MkdirAll(sourcemapDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create sourcemap directory: %w", err)
	}

	mgr := &Manager{
		dataDir: sourcemapDir,
		cache:   make(map[string]*RawSourceMap),
		meta:    make(map[string][]model.SourceMapMeta),
	}

	_ = mgr.loadExistingMeta()
	return mgr, nil
}

func (m *Manager) loadExistingMeta() error {
	entries, err := os.ReadDir(m.dataDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			relVersion := entry.Name()
			relDir := filepath.Join(m.dataDir, relVersion)
			files, _ := os.ReadDir(relDir)
			for _, f := range files {
				if !f.IsDir() && strings.HasSuffix(f.Name(), ".map") {
					info, _ := f.Info()
					m.meta[relVersion] = append(m.meta[relVersion], model.SourceMapMeta{
						ReleaseVersion: relVersion,
						Filename:       f.Name(),
						SizeBytes:      info.Size(),
						UploadedAt:     info.ModTime().UnixMilli(),
					})
				}
			}
		}
	}
	return nil
}

func (m *Manager) SaveSourceMap(releaseVersion, filename string, content []byte) (*model.SourceMapMeta, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	relDir := filepath.Join(m.dataDir, releaseVersion)
	if err := os.MkdirAll(relDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create release sourcemap dir: %w", err)
	}

	targetPath := filepath.Join(relDir, filename)
	if err := os.WriteFile(targetPath, content, 0644); err != nil {
		return nil, fmt.Errorf("failed to write sourcemap file: %w", err)
	}

	var sm RawSourceMap
	if err := json.Unmarshal(content, &sm); err == nil {
		cacheKey := releaseVersion + ":" + filename
		m.cache[cacheKey] = &sm
		trimmedName := strings.TrimSuffix(filename, ".map")
		m.cache[releaseVersion+":"+trimmedName] = &sm
	}

	meta := model.SourceMapMeta{
		ReleaseVersion: releaseVersion,
		Filename:       filename,
		SizeBytes:      int64(len(content)),
		UploadedAt:     time.Now().UnixMilli(),
	}

	existing := m.meta[releaseVersion]
	updated := false
	for i, item := range existing {
		if item.Filename == filename {
			existing[i] = meta
			updated = true
			break
		}
	}
	if !updated {
		m.meta[releaseVersion] = append(existing, meta)
	}

	return &meta, nil
}

func (m *Manager) ListSourceMaps(releaseVersion string) []model.SourceMapMeta {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.meta[releaseVersion]
}

func (m *Manager) GetSourceMap(releaseVersion, filename string) (*RawSourceMap, error) {
	cacheKey := releaseVersion + ":" + filename
	m.mu.RLock()
	sm, ok := m.cache[cacheKey]
	m.mu.RUnlock()
	if ok && sm != nil {
		return sm, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if sm, ok := m.cache[cacheKey]; ok && sm != nil {
		return sm, nil
	}

	relDir := filepath.Join(m.dataDir, releaseVersion)
	baseFilename := filepath.Base(filename)
	possibleNames := []string{
		baseFilename,
		baseFilename + ".map",
		strings.TrimSuffix(baseFilename, ".map"),
	}

	for _, name := range possibleNames {
		filePath := filepath.Join(relDir, name)
		content, err := os.ReadFile(filePath)
		if err == nil {
			var parsed RawSourceMap
			if err := json.Unmarshal(content, &parsed); err == nil {
				m.cache[cacheKey] = &parsed
				return &parsed, nil
			}
		}
	}

	return nil, fmt.Errorf("source map not found for release %s and file %s", releaseVersion, filename)
}

func (m *Manager) UnminifyStacktrace(releaseVersion string, minifiedFrames []model.StackFrame) []model.UnminifiedFrame {
	unminified := make([]model.UnminifiedFrame, len(minifiedFrames))

	for i, frame := range minifiedFrames {
		sm, err := m.GetSourceMap(releaseVersion, frame.Filename)
		if err != nil || sm == nil {
			unminified[i] = model.UnminifiedFrame{
				Filename:    frame.Filename,
				Function:    frame.Function,
				Lineno:      frame.Lineno,
				Colno:       frame.Colno,
				CodeContext: []string{fmt.Sprintf("// Source map pending for %s", filepath.Base(frame.Filename))},
			}
			continue
		}

		origFile, origLine, origCol, origFunc, contextLines := m.resolveFrameMapping(sm, frame.Lineno, frame.Colno, frame.Function)

		unminified[i] = model.UnminifiedFrame{
			Filename:    origFile,
			Function:    origFunc,
			Lineno:      origLine,
			Colno:       origCol,
			CodeContext: contextLines,
		}
	}

	return unminified
}

func (m *Manager) resolveFrameMapping(sm *RawSourceMap, line, col int, fallbackFunc string) (string, int, int, string, []string) {
	mappings := DecodeVLQMappings(sm, sm.Mappings)

	var bestMatch *DecodedMapping
	for i := range mappings {
		m := &mappings[i]
		if m.GeneratedLine == line {
			if m.GeneratedColumn <= col {
				if bestMatch == nil || m.GeneratedColumn > bestMatch.GeneratedColumn {
					bestMatch = m
				}
			}
		}
	}

	if bestMatch == nil || bestMatch.OriginalSource == "" {
		origSource := "src/index.js"
		if len(sm.Sources) > 0 {
			origSource = sm.Sources[0]
		}
		context := extractContextFromSource(sm, origSource, line, 5)
		return origSource, line, col, fallbackFunc, context
	}

	funcName := fallbackFunc
	if bestMatch.Name != "" {
		funcName = bestMatch.Name
	}

	context := extractContextFromSource(sm, bestMatch.OriginalSource, bestMatch.OriginalLine, 5)
	return bestMatch.OriginalSource, bestMatch.OriginalLine, bestMatch.OriginalColumn, funcName, context
}

func extractContextFromSource(sm *RawSourceMap, sourcePath string, line int, radius int) []string {
	var fullSource string
	for i, s := range sm.Sources {
		if s == sourcePath || strings.HasSuffix(s, sourcePath) || strings.HasSuffix(sourcePath, s) {
			if i < len(sm.SourcesContent) {
				fullSource = sm.SourcesContent[i]
				break
			}
		}
	}

	if fullSource == "" && len(sm.SourcesContent) > 0 {
		fullSource = sm.SourcesContent[0]
	}

	if fullSource == "" {
		return []string{"// Source code context unavailable"}
	}

	lines := strings.Split(fullSource, "\n")
	if line <= 0 || line > len(lines) {
		if len(lines) > 0 {
			max := 5
			if len(lines) < max {
				max = len(lines)
			}
			return lines[:max]
		}
		return []string{"// Line out of bounds"}
	}

	start := line - radius
	if start < 0 {
		start = 0
	}
	end := line + radius
	if end > len(lines) {
		end = len(lines)
	}

	return lines[start:end]
}

const vlqBaseShift = 5
const vlqBase = 1 << vlqBaseShift
const vlqBaseMask = vlqBase - 1
const vlqContinuationBit = vlqBase

var base64Digits = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
var base64Values [256]int

func init() {
	for i := 0; i < 256; i++ {
		base64Values[i] = -1
	}
	for i := 0; i < len(base64Digits); i++ {
		base64Values[base64Digits[i]] = i
	}
}

func DecodeVLQMappings(sm *RawSourceMap, mappings string) []DecodedMapping {
	var result []DecodedMapping

	generatedLine := 1
	generatedCol := 0
	sourcesIndex := 0
	originalLine := 1
	originalCol := 0
	namesIndex := 0

	lines := strings.Split(mappings, ";")
	for _, lineStr := range lines {
		generatedCol = 0
		if lineStr == "" {
			generatedLine++
			continue
		}

		segments := strings.Split(lineStr, ",")
		for _, seg := range segments {
			if seg == "" {
				continue
			}

			fields, ok := decodeVLQSegment(seg)
			if !ok || len(fields) == 0 {
				continue
			}

			generatedCol += fields[0]
			mapping := DecodedMapping{
				GeneratedLine:   generatedLine,
				GeneratedColumn: generatedCol,
			}

			if len(fields) >= 4 {
				sourcesIndex += fields[1]
				originalLine += fields[2]
				originalCol += fields[3]

				// Resolve the real source filename from the sources array
				if sourcesIndex >= 0 && sourcesIndex < len(sm.Sources) {
					mapping.OriginalSource = sm.Sources[sourcesIndex]
				}
				mapping.OriginalLine = originalLine
				mapping.OriginalColumn = originalCol

				if len(fields) >= 5 {
					namesIndex += fields[4]
					// Resolve the real function name from the names array
					if namesIndex >= 0 && namesIndex < len(sm.Names) {
						mapping.Name = sm.Names[namesIndex]
					}
				}
			}

			result = append(result, mapping)
		}
		generatedLine++
	}

	return result
}

func decodeVLQSegment(seg string) ([]int, bool) {
	var fields []int
	idx := 0
	for idx < len(seg) {
		var result int
		shift := 0
		continuation := true

		for continuation {
			if idx >= len(seg) {
				return nil, false
			}
			char := seg[idx]
			idx++

			val := base64Values[char]
			if val < 0 {
				return nil, false
			}

			continuation = (val & vlqContinuationBit) != 0
			digit := val & vlqBaseMask
			result += digit << shift
			shift += vlqBaseShift
		}

		isNegative := (result & 1) == 1
		value := result >> 1
		if isNegative {
			value = -value
		}
		fields = append(fields, value)
	}
	return fields, true
}
