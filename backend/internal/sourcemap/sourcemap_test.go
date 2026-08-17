package sourcemap

import (
	"os"
	"testing"

	"SigTrap-backend/internal/model"
)

func TestSourceMapUnminify(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "sourcemap_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	mgr, err := NewManager(tempDir)
	if err != nil {
		t.Fatalf("failed to create sourcemap manager: %v", err)
	}

	sampleSourceMap := `{
		"version": 3,
		"file": "main.min.js",
		"sources": ["src/components/List.tsx"],
		"sourcesContent": ["import React from 'react';\n\nexport const List = ({ items }) => {\n  return (\n    <div className='list'>\n      {items.map(i => <div key={i.id}>{i.name}</div>)}\n    </div>\n  );\n};"],
		"names": ["renderList", "items", "map"],
		"mappings": "AAAA,MAAMA,..."
	}`

	releaseVersion := "v1.4.2-ab89c2"
	meta, err := mgr.SaveSourceMap(releaseVersion, "main.min.js.map", []byte(sampleSourceMap))
	if err != nil {
		t.Fatalf("failed to save sourcemap: %v", err)
	}
	if meta.ReleaseVersion != releaseVersion {
		t.Errorf("expected release_version %s, got %s", releaseVersion, meta.ReleaseVersion)
	}

	// Test un-minification of minified frames
	minifiedFrames := []model.StackFrame{
		{Filename: "https://segv.tech/assets/main.min.js", Function: "renderList", Lineno: 1, Colno: 4892},
	}

	unminified := mgr.UnminifyStacktrace(releaseVersion, minifiedFrames)
	if len(unminified) != 1 {
		t.Fatalf("expected 1 unminified frame, got %d", len(unminified))
	}

	if unminified[0].Filename == "" {
		t.Errorf("expected non-empty unminified filename")
	}
	if len(unminified[0].CodeContext) == 0 {
		t.Errorf("expected non-empty code context snippet")
	}
}

func TestVLQDecoder(t *testing.T) {
	mappings := DecodeVLQMappings("AAAA,MAAMA;")
	if len(mappings) == 0 {
		t.Fatalf("expected decoded mappings, got 0")
	}
}
