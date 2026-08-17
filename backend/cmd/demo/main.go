package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

func main() {
	baseURL := "http://localhost:8080"
	projectKey := "123e4567-e89b-12d3-a456-426614174000"
	adminToken := "dev_admin_secret_123"

	// 1. Health check
	resp, err := http.Get(baseURL + "/api/v1/health")
	if err != nil {
		fmt.Printf("health check failed: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("Health Check (%d):\n%s\n\n", resp.StatusCode, string(body))

	// 2. Upload sourcemap
	sourceMapContent := `{
		"version": 3,
		"file": "main.min.js",
		"sources": ["src/components/List.tsx"],
		"sourcesContent": [
			"import React from 'react';\nimport { ListItem } from './ListItem';\n\nexport const List = ({ items }) => {\n  return (\n    <div className='list-wrapper'>\n      {items.map(item => (\n        <ListItem key={item.id} {...item} />\n      ))}\n    </div>\n  );\n};\n"
		],
		"names": ["renderList", "items", "map"],
		"mappings": "AAAA,MAAMA,..."
	}`

	bodyBuf := &bytes.Buffer{}
	writer := multipart.NewWriter(bodyBuf)
	_ = writer.WriteField("release_version", "v1.4.2-ab89c2")
	part, _ := writer.CreateFormFile("file", "main.min.js.map")
	_, _ = part.Write([]byte(sourceMapContent))
	_ = writer.Close()

	req, _ := http.NewRequest("POST", baseURL+"/api/v1/artifacts/sourcemaps", bodyBuf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+adminToken)

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("sourcemap upload failed: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	fmt.Printf("Source Map Upload (%d):\n%s\n\n", resp.StatusCode, string(body))

	// 3. Post telemetry payload
	trapPayload := map[string]interface{}{
		"event_id":        "123e4567-e89b-12d3-a456-426614174000",
		"timestamp":       time.Now().UnixMilli(),
		"release_version": "v1.4.2-ab89c2",
		"environment":     "production",
		"exception": map[string]interface{}{
			"type":  "TypeError",
			"value": "Cannot read properties of undefined (reading 'map')",
			"stacktrace": []map[string]interface{}{
				{
					"filename": "https://segv.tech/assets/main.min.js",
					"function": "renderList",
					"lineno":   1,
					"colno":    4892,
				},
			},
		},
		"breadcrumbs": []map[string]interface{}{
			{
				"timestamp": time.Now().UnixMilli() - 3000,
				"category":  "ui.click",
				"message":   "div#submit-button.btn-primary",
				"data":      map[string]interface{}{},
			},
			{
				"timestamp": time.Now().UnixMilli() - 1500,
				"category":  "network.fetch",
				"message":   "POST /api/checkout",
				"data": map[string]interface{}{
					"status_code": 500,
					"latency_ms":  150,
				},
			},
		},
		"context": map[string]interface{}{
			"browser": "Chrome 115.0.0.0",
			"os":      "Windows 11",
			"url":     "https://segv.tech/checkout",
			"viewport": map[string]interface{}{
				"width":  1920,
				"height": 1080,
			},
		},
		"user": map[string]interface{}{
			"id":    "usr_9981",
			"email": "dev@segv.tech",
		},
	}

	jsonBytes, _ := json.Marshal(trapPayload)
	req, _ = http.NewRequest("POST", baseURL+"/api/v1/trap", bytes.NewBuffer(jsonBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SigTrap-Project-Key", projectKey)

	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("trap ingestion failed: %v\n", err)
		return
	}
	defer resp.Body.Close()
	fmt.Printf("Ingestion Status: %d\n\n", resp.StatusCode)

	time.Sleep(300 * time.Millisecond)

	// 4. Query issues
	resp, err = http.Get(baseURL + "/api/v1/issues?env=production")
	if err != nil {
		fmt.Printf("query issues failed: %v\n", err)
		return
	}
	defer resp.Body.Close()
	body, _ = io.ReadAll(resp.Body)
	fmt.Printf("Issues (%d):\n%s\n\n", resp.StatusCode, string(body))

	var issuesList struct {
		Issues []struct {
			IssueID string `json:"issue_id"`
		} `json:"issues"`
	}
	_ = json.Unmarshal(body, &issuesList)

	if len(issuesList.Issues) > 0 {
		issueID := issuesList.Issues[0].IssueID

		// 5. Query diagnostic
		resp, err = http.Get(fmt.Sprintf("%s/api/v1/issues/%s/diagnostic", baseURL, issueID))
		if err != nil {
			fmt.Printf("diagnostic query failed: %v\n", err)
			return
		}
		defer resp.Body.Close()
		body, _ = io.ReadAll(resp.Body)

		var prettyJSON bytes.Buffer
		_ = json.Indent(&prettyJSON, body, "", "  ")
		fmt.Printf("Diagnostic (%d):\n%s\n\n", resp.StatusCode, prettyJSON.String())
	}
}
