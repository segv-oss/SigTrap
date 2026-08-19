# ⚡ SigTrap User Guide & Documentation

> **Catch the signal. Replay the state. Patch the root cause.**

Welcome to **SigTrap**, a developer-first runtime telemetry and post-mortem debugging platform. SigTrap hooks into web applications to trap unhandled runtime exceptions, record a rolling ring buffer of user and network breadcrumbs, map minified stack traces back to original source code, and run AI-assisted root-cause diagnosis with ready-to-apply code patches.

---

## 📑 Table of Contents

1. [Overview & Core Architecture](#-overview--core-architecture)
2. [Getting Started & Local Setup](#-getting-started--local-setup)
3. [Integrating the SigTrap SDK in Your Web Application](#-integrating-the-sigtrap-sdk-in-your-web-application)
   - [Basic Initialization](#basic-initialization)
   - [Automatic Telemetry Capture](#automatic-telemetry-capture)
   - [Manual Exception & Breadcrumb Logging](#manual-exception--breadcrumb-logging)
   - [React Error Boundary Integration](#react-error-boundary-integration)
4. [Source Maps & Unminification Pipeline](#-source-maps--unminification-pipeline)
   - [Uploading Source Maps via CI/CD](#uploading-source-maps-via-cicd)
   - [Managing Uploaded Source Maps](#managing-uploaded-source-maps)
5. [Using the SigTrap Cockpit (Dashboard)](#-using-the-sigtrap-cockpit-dashboard)
   - [Issue Navigation & Filtering](#issue-navigation--filtering)
   - [AI Root-Cause Diagnosis & Instant Patches](#ai-root-cause-diagnosis--instant-patches)
   - [Unminified Stack Trace Explorer](#unminified-stack-trace-explorer)
   - [Breadcrumb Ring Buffer Replay](#breadcrumb-ring-buffer-replay)
   - [Runtime Environment Registers](#runtime-environment-registers)
   - [Issue Lifecycle Actions](#issue-lifecycle-actions)
6. [Backend Configuration & Operations](#-backend-configuration--operations)
   - [Environment Variables](#environment-variables)
   - [Health & Operational Metrics](#health--operational-metrics)
   - [Data Persistence](#data-persistence)
7. [Troubleshooting & FAQs](#-troubleshooting--faqs)

---

## 🧠 Overview & Core Architecture

Traditional logging tools record isolated error messages without the historical sequence of interactions that caused them. **SigTrap** treats client-side crashes like an OS breakpoint trap:

```text
  ┌────────────────────────────────────────────────────────┐
  │                 Client Web Application                 │
  │  (Auto-captures: Clicks, Fetches, Errors, Rejections)  │
  └───────────────────────────┬────────────────────────────┘
                              │ HTTP POST /api/v1/trap (Non-blocking)
                              ▼
  ┌────────────────────────────────────────────────────────┐
  │                   Go Ingestion Node                    │
  │  - Rate Limiting per Project Key                       │
  │  - Async Worker Pool                                   │
  │  - Enforces 50-Item Breadcrumb Ring Buffer             │
  └─────────────┬────────────────────────────┬─────────────┘
                │                            │
                ▼                            ▼
  ┌───────────────────────────┐  ┌───────────────────────────┐
  │   Source Map VLQ Engine   │  │   AI Diagnostic Engine    │
  │   (Minified -> Source)    │  │   (Gemini + Fallback Heur.)│
  └─────────────┬─────────────┘  └───────────┬───────────────┘
                │                            │
                └─────────────┬──────────────┘
                              ▼
  ┌────────────────────────────────────────────────────────┐
  │             SigTrap Cockpit (React + Vite)             │
  │  - Real-time Issue Feed & Sparklines                   │
  │  - Visual Breadcrumb Timeline                          │
  │  - 1-Click AI Diff Patch Applicator                    │
  └────────────────────────────────────────────────────────┘
```

---

## 🚀 Getting Started & Local Setup

### Prerequisites

| Requirement | Minimum Version | Notes |
|---|---|---|
| **Go** | 1.21+ | Required for backend ingestion server |
| **Node.js** | 18.0+ | Required for Cockpit and SDK build |
| **npm** | 9.0+ | Node package manager |

### Step 1: Clone and Configure Environment

```bash
git clone https://github.com/your-org/SigTrap.git
cd SigTrap

# Create your local environment configuration
cp .env.example .env
```

Edit `.env` to configure your settings:

```env
# Backend server port
PORT=8080

# Secret token used by CI/CD to upload sourcemaps
SIGTRAP_ADMIN_TOKEN=dev_admin_secret_123

# Data persistence directory
SIGTRAP_DATA_DIR=./data

# (Optional) Gemini API Key for LLM root-cause analysis and diff generation.
# If omitted, SigTrap automatically falls back to the built-in Heuristic Engine.
GEMINI_API_KEY=your_gemini_api_key_here
```

### Step 2: Install Dependencies

```bash
make install
```

This downloads Go module dependencies for the backend and installs npm packages for the frontend.

### Step 3: Launch SigTrap (Backend + Cockpit UI)

Run both the Go ingestion backend and the React Cockpit concurrently:

```bash
make dev
```

* **Ingestion API & Backend:** `http://localhost:8080`
* **Debugging Cockpit UI:** `http://localhost:5173`

### Step 4: Fire a Test Trap

1. Open `http://localhost:5173` in your browser.
2. Check that the top status pill indicates **Backend online** (green dot).
3. Click the **💥 Fire Test Trap** button in the top right.
4. A simulated `TypeError` with breadcrumbs will be ingested, mapped, analyzed by the AI engine, and displayed in the sidebar within seconds.

---

## 📦 Integrating the SigTrap SDK in Your Web Application

The SigTrap Browser SDK is lightweight, zero-dependency, and records user interactions in a rolling memory buffer.

### Basic Initialization

Import and initialize `SigTrap` at the earliest entry point of your application (e.g., `main.tsx`, `index.js`):

```typescript
import { SigTrap } from './sdk/sigtrap';

SigTrap.init({
  projectKey: '123e4567-e89b-12d3-a456-426614174000',
  endpoint: 'http://localhost:8080/api/v1/trap',
  environment: 'production',         // 'production' | 'staging' | 'development'
  releaseVersion: 'v1.4.2-ab89c2',   // Must match your source map build version
});
```

### Automatic Telemetry Capture

Once initialized, SigTrap automatically attaches listeners to your browser runtime:

1. **`window.onerror`**: Intercepts unhandled JavaScript runtime exceptions, extracts the stack trace, and ships the payload.
2. **`unhandledrejection`**: Catches unhandled Promise rejections and network errors.
3. **DOM Interaction Tracking**: Intercepts DOM click events and appends them to the breadcrumb ring buffer (e.g., `button#submit-form.btn-primary`).
4. **`window.fetch` Monkey-Patching**: Intercepts outgoing HTTP requests and records the method, URL, HTTP status code, and latency in milliseconds.

### Manual Exception & Breadcrumb Logging

You can manually record custom breadcrumbs and capture handled exceptions:

```typescript
import { SigTrap } from './sdk/sigtrap';

// 1. Manually add custom telemetry breadcrumbs
SigTrap.addBreadcrumb({
  category: 'state.mutation',
  message: 'USER_LOGGED_IN',
  data: { role: 'admin', teamId: 'team_42' },
});

// 2. Capture handled errors in try-catch blocks
try {
  riskyOperation();
} catch (error) {
  SigTrap.captureException(error as Error, 'CustomOperationError');
}
```

### React Error Boundary Integration

To catch React render-phase errors gracefully:

```tsx
import React, { Component, ErrorInfo, ReactNode } from 'react';
import { SigTrap } from './sdk/sigtrap';

interface Props {
  children: ReactNode;
  fallback?: ReactNode;
}

interface State {
  hasError: boolean;
}

export class SigTrapErrorBoundary extends Component<Props, State> {
  public state: State = { hasError: false };

  public static getDerivedStateFromError(_: Error): State {
    return { hasError: true };
  }

  public componentDidCatch(error: Error, errorInfo: ErrorInfo) {
    SigTrap.addBreadcrumb({
      category: 'react.lifecycle',
      message: 'ErrorBoundary triggered',
      data: { componentStack: errorInfo.componentStack },
    });
    SigTrap.captureException(error, 'ReactErrorBoundaryException');
  }

  public render() {
    if (this.state.hasError) {
      return this.props.fallback || <h2>Something went wrong. An incident has been logged.</h2>;
    }
    return this.props.children;
  }
}
```

---

## 🗺️ Source Maps & Unminification Pipeline

When production builds minify JavaScript bundles into single lines (e.g., `main.min.js:1:4892`), SigTrap uses Source Map v3 artifacts with a VLQ decoder to reconstruct exact file paths, line numbers, function names, and enclosing code blocks.

### Uploading Source Maps via CI/CD

During your deployment/build pipeline, upload your generated `.js.map` files to SigTrap:

```bash
curl -X POST http://localhost:8080/api/v1/artifacts/sourcemaps \
  -H "Authorization: Bearer <SIGTRAP_ADMIN_TOKEN>" \
  -F "release_version=v1.4.2-ab89c2" \
  -F "file=@dist/assets/index-SSe8N4im.js.map"
```

> ⚠️ **Important:** The `release_version` passed in `curl` must **strictly match** the `releaseVersion` specified in `SigTrap.init({...})`.

### Managing Uploaded Source Maps

To inspect all indexed source maps stored on the backend:

```bash
curl -X GET http://localhost:8080/api/v1/artifacts/sourcemaps \
  -H "Authorization: Bearer <SIGTRAP_ADMIN_TOKEN>"
```

**Sample Response:**
```json
{
  "sourcemaps": [
    {
      "release_version": "v1.4.2-ab89c2",
      "filename": "index-SSe8N4im.js.map",
      "size_bytes": 482012,
      "uploaded_at": 1723827000000
    }
  ]
}
```

---

## 🖥️ Using the SigTrap Cockpit (Dashboard)

The Cockpit (`http://localhost:5173`) provides an ultra-dense, engineer-focused triage interface.

### Issue Navigation & Filtering

* **Search Bar:** Filter issues by exception name (`TypeError`), message text (`reading 'map'`), or culprit source file (`List.tsx`).
* **Status Tabs:** Filter between **unresolved**, **resolved**, **ignored**, or **all** issues.
* **Environment Selector:** Switch between `production`, `staging`, or `all envs`.
* **Issue Cards:** Show event velocity, impacted user count, last seen relative timestamp, and an event trend sparkline.

### AI Root-Cause Diagnosis & Instant Patches

When you select an issue:
1. SigTrap provides an AI-generated explanation explaining *why* the crash happened in context with upstream network requests or empty props.
2. A confidence score percentage is computed.
3. A unified `git diff` patch is suggested.
4. Click **⎘ Copy diff** to copy the ready-to-apply patch directly to your clipboard.
5. Click **↻ Re-run AI** to re-analyze the latest telemetry if additional crash events have arrived.

### Unminified Stack Trace Explorer

* Displays the full stack frame hierarchy.
* Shows the unminified source file, line number, column number, and enclosing function.
* Displays syntax-highlighted code context with hot-line detection (e.g. highlighting missing null-checks).

### Breadcrumb Ring Buffer Replay

* Displays the exact chronological trail of up to **50 user interactions, network requests, and navigation events** that occurred right before the crash.
* Shows request method, path, HTTP response status (e.g. `POST /api/checkout [500]`), and roundtrip latency in milliseconds.

### Runtime Environment Registers

Inspect hardware and client registers captured at the exact millisecond of failure:
* **Browser:** User agent and browser engine.
* **OS:** Host operating system.
* **Viewport:** Screen dimensions (width × height).
* **Crash URL:** Exact web page URL where the trap triggered.

### Issue Lifecycle Actions

* **✓ Resolve:** Marks the issue as fixed. It will be hidden from the default unresolved feed unless it regresses in a newer release.
* **Ignore:** Mutes notifications and archives the issue card.

---

## ⚙️ Backend Configuration & Operations

### Environment Variables

| Variable | Type | Default | Description |
|---|---|---|---|
| `PORT` | Integer | `8080` | Port for the HTTP API server |
| `SIGTRAP_ADMIN_TOKEN` | String | `dev_admin_secret_123` | Bearer token for admin routes (sourcemap uploads) |
| `SIGTRAP_DATA_DIR` | String | `./data` | Filepath for disk state persistence & sourcemap storage |
| `SIGTRAP_MAX_BREADCRUMBS`| Integer | `50` | Maximum breadcrumb history capacity per trap |
| `SIGTRAP_WORKER_COUNT` | Integer | `4` | Number of concurrent background processing goroutines |
| `SIGTRAP_BUFFER_SIZE` | Integer | `1000` | Ingestion channel buffer capacity before backpressure |
| `SIGTRAP_RATE_LIMIT_RPS` | Float | `100.0` | Ingestion rate limit (requests/sec per Project Key) |
| `GEMINI_API_KEY` | String | `""` | Google Gemini API key for AI diagnostic engine |

### Health & Operational Metrics

SigTrap includes an operational health endpoint for monitoring and uptime checks:

```bash
curl http://localhost:8080/api/v1/health
```

**Response:**
```json
{
  "status": "healthy",
  "service": "sigtrap-backend",
  "version": "1.0.0",
  "timestamp": 1723827484000,
  "metrics": {
    "events_ingested": 14205,
    "active_issues": 18,
    "queue_depth": 0,
    "worker_count": 8
  }
}
```

### Data Persistence

SigTrap stores aggregated issue groupings and historical metadata in `$SIGTRAP_DATA_DIR/state.json`. Source maps are saved and indexed under `$SIGTRAP_DATA_DIR/sourcemaps/`. State is automatically saved on graceful shutdown (`SIGINT` / `SIGTERM`) and loaded on startup.

---

## ❓ Troubleshooting & FAQs

### 1. The Cockpit shows "Backend offline"
* Verify the backend server is running (`make dev-backend` or `make dev`).
* Check that port `8080` is not in use by another process.
* Confirm `http://localhost:8080/api/v1/health` returns `{"status":"healthy"}`.

### 2. Stack traces show minified filenames (`main.min.js:1:4892`)
* Make sure you uploaded the source map for that exact release.
* Verify that the `releaseVersion` set in `SigTrap.init()` exactly matches the `release_version` uploaded in the `curl` multipart request.

### 3. AI diagnostics show fallback/heuristic messages
* Verify that `GEMINI_API_KEY` is set in your `.env` file and that you restarted the backend.
* Check your internet connection or verify your Gemini API quota. SigTrap automatically falls back to heuristic diagnosis if the AI API is unreachable.

### 4. SDK sends events but backend returns `429 Too Many Requests`
* Ingestion is rate-limited per `X-SigTrap-Project-Key` to 100 RPS by default.
* Increase `SIGTRAP_RATE_LIMIT_RPS` in your `.env` if you are load testing.

---

## 📄 License & Attribution

SigTrap is built with ❤️ by team **SEGV**.  
Catch the signal. Replay the state. Patch the root cause.
