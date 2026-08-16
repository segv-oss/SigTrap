# ⚡ SIGTRAP API Contract

> **Catch the signal. Replay the state. Patch the root cause.**
> Version: 1.1.0 (Production-Grade Telemetry & Cockpit Specification)

This document defines the production API contract between the **SIGTRAP Browser SDK**, **Go Backend Ingestion Node**, **Source Map Artifact Pipeline**, and **React Debugging Cockpit**.

---

## 1. Global Standards & Operational Rules

### 1.1 Authentication & Authorization Matrix

| Endpoint Route | Auth Type | Header | Required | Notes |
| :--- | :--- | :--- | :--- | :--- |
| `POST /api/v1/trap` | Public Key | `X-SigTrap-Project-Key: <uuid>` | Yes | SDK client key. Rate limited per key. |
| `POST /api/v1/artifacts/sourcemaps` | Admin Bearer | `Authorization: Bearer <secret>` | Yes | Source map upload by CI/CD pipeline. |
| `GET /api/v1/issues*` | Session / API | `X-SigTrap-Project-Key: <uuid>` | Yes | Cockpit reader access. |
| `PATCH /api/v1/issues/*` | Session / API | `X-SigTrap-Project-Key: <uuid>` | Yes | Cockpit state mutation. |
| `GET /api/v1/health` | None | N/A | No | Liveness and operational metrics. |

### 1.2 Rate Limiting & Response Headers
- Ingestion endpoints enforce rate limits per project key (default: 100 requests/second with burst allowance of 200).
- Standard rate limit response headers:
  - `X-RateLimit-Limit`: Maximum requests per window.
  - `X-RateLimit-Remaining`: Remaining request capacity.
  - `X-RateLimit-Reset`: Unix timestamp when capacity resets.
- HTTP Status `429 Too Many Requests` is returned when rate limits are exceeded.

### 1.3 Universal Standard Error Payload
All non-2xx API error responses conform to this JSON schema:

```json
{
  "error": {
    "code": "INVALID_PAYLOAD",
    "message": "Field 'timestamp' must be a numeric Unix timestamp in milliseconds.",
    "details": [
      {
        "field": "timestamp",
        "issue": "expected number, received string"
      }
    ]
  }
}
```

---

## 2. Firehose: SDK Ingestion API

### Endpoint: `POST /api/v1/trap`

**Headers:**
```http
X-SigTrap-Project-Key: 123e4567-e89b-12d3-a456-426614174000
Content-Type: application/json
```

### Request Payload

```json
{
  "event_id": "123e4567-e89b-12d3-a456-426614174000",
  "timestamp": 1723827484000,
  "release_version": "v1.4.2-ab89c2",
  "environment": "production",
  "exception": {
    "type": "TypeError",
    "value": "Cannot read properties of undefined (reading 'map')",
    "stacktrace": [
      {
        "filename": "https://segv.tech/assets/main.min.js",
        "function": "renderList",
        "lineno": 1,
        "colno": 4892
      }
    ]
  },
  "breadcrumbs": [
    {
      "timestamp": 1723827481000,
      "category": "ui.click",
      "message": "div#submit-button.btn-primary",
      "data": {}
    },
    {
      "timestamp": 1723827482500,
      "category": "network.fetch",
      "message": "POST /api/checkout",
      "data": {
        "status_code": 500,
        "latency_ms": 150
      }
    }
  ],
  "context": {
    "browser": "Chrome 115.0.0.0",
    "os": "Windows 11",
    "url": "https://segv.tech/checkout",
    "viewport": {
      "width": 1920,
      "height": 1080
    }
  },
  "user": {
    "id": "usr_9981",
    "email": "dev@segv.tech"
  },
  "sdk": {
    "name": "sigtrap-browser",
    "version": "1.0.0"
  }
}
```

### Contract Specification

| Field | Type | Required | Description |
| :--- | :--- | :--- | :--- |
| `event_id` | `string` | Yes | Valid UUID v4 identifying the crash event |
| `timestamp` | `number` | Yes | Unix timestamp in milliseconds |
| `release_version` | `string` | Yes | Application release/build identifier |
| `environment` | `string` | Yes | Environment (`production`, `staging`, `development`) |
| `exception.type` | `string` | Yes | Exception/error class name (e.g. `TypeError`, `UnhandledRejection`) |
| `exception.value` | `string` | Yes | Human-readable exception message |
| `exception.stacktrace` | `array` | Yes | Array of frame objects containing `filename`, `function`, `lineno`, `colno` |
| `stacktrace[].filename` | `string` | Yes | Source file URL or generated minified bundle path |
| `stacktrace[].function` | `string` | No | Function name containing the frame |
| `stacktrace[].lineno` | `number` | Yes | 1-indexed line number |
| `stacktrace[].colno` | `number` | Yes | 1-indexed column number |
| `breadcrumbs` | `array` | Yes | Ring-buffered events array preceding the crash |
| `breadcrumbs[].timestamp` | `number` | Yes | Unix timestamp in milliseconds |
| `breadcrumbs[].category` | `string` | Yes | Event category (`ui.click`, `network.fetch`, `navigation`, `console.error`) |
| `breadcrumbs[].message` | `string` | Yes | Human-readable event description |
| `breadcrumbs[].data` | `object` | No | Structured metadata key-values |
| `context.browser` | `string` | Yes | Browser name and major version |
| `context.os` | `string` | Yes | Operating system |
| `context.url` | `string` | Yes | Web page URL where crash occurred |
| `context.viewport.width` | `number` | Yes | Browser window width in pixels |
| `context.viewport.height`| `number` | Yes | Browser window height in pixels |
| `user.id` | `string` | No | Optional identifier for user impact deduplication |
| `user.email` | `string` | No | Optional user email |
| `sdk.name` | `string` | No | SDK library identifier |
| `sdk.version` | `string` | No | SDK library version |

### 🛡️ Breadcrumb Ring Buffer Guarantee
- The SDK **must not** transmit more than **50 breadcrumbs**.
- The Go backend **strictly enforces a 50-item ring buffer**. If payloads arrive with >50 items, the backend automatically retains only the **latest 50 breadcrumbs** prior to processing.

### ⚡ Backend Non-Blocking Ingestion Behavior
The Go ingestion node:
1. Validates project key & payload schema asynchronously.
2. Enqueues the event into an internal buffered Go channel.
3. Returns **`202 Accepted`** with an empty body **immediately** (<5ms response latency). DB persistence, stack frame unminification, and AI analysis execute in background worker goroutines.

---

## 3. Artifacts: Source Map Management API

### 3.1 Upload Source Map
**Endpoint:** `POST /api/v1/artifacts/sourcemaps`

**Headers:**
```http
Authorization: Bearer <secret_admin_token>
Content-Type: multipart/form-data
```

**Multipart Fields:**

| Field | Type | Description |
| :--- | :--- | :--- |
| `release_version` | `string` (Text) | Application release identifier (must match SDK `release_version`) |
| `file` | `file` (Binary) | Standard Source Map v3 file (`.map` or `.js.map`) |

**Response (201 Created):**
```json
{
  "status": "success",
  "message": "Source map successfully uploaded and indexed.",
  "release_version": "v1.4.2-ab89c2",
  "filename": "main.min.js.map",
  "files_mapped": 42
}
```

### 3.2 List Active Source Maps
**Endpoint:** `GET /api/v1/artifacts/sourcemaps`

**Headers:**
```http
Authorization: Bearer <secret_admin_token>
```

**Response (200 OK):**
```json
{
  "sourcemaps": [
    {
      "release_version": "v1.4.2-ab89c2",
      "filename": "main.min.js.map",
      "size_bytes": 482012,
      "uploaded_at": 1723827000000
    }
  ]
}
```

---

## 4. Cockpit Dashboard APIs

### 4.1 List & Filter Aggregated Issues

**Endpoint:** `GET /api/v1/issues`

**Query Parameters:**

| Parameter | Type | Required | Default | Description |
| :--- | :--- | :--- | :--- | :--- |
| `project_id` | `string` | Yes | N/A | Project identifier |
| `env` | `string` | No | `production` | Filter by deployment environment |
| `status` | `string` | No | `unresolved` | Filter by issue state (`unresolved`, `resolved`, `ignored`, `all`) |
| `search` | `string` | No | None | Free-text search matching exception type, message, or culprit file |
| `limit` | `integer`| No | `50` | Maximum issues to return (max 100) |
| `offset` | `integer`| No | `0` | Pagination offset |

**Response (200 OK):**
```json
{
  "issues": [
    {
      "issue_id": "grp_98234",
      "type": "TypeError",
      "value": "Cannot read properties of undefined (reading 'map')",
      "culprit_file": "src/components/List.tsx",
      "event_count": 482,
      "users_affected": 12,
      "last_seen": 1723827484000,
      "first_seen": 1723820000000,
      "status": "unresolved",
      "release_version": "v1.4.2-ab89c2"
    }
  ],
  "total": 1,
  "limit": 50,
  "offset": 0
}
```

---

### 4.2 Mutate Issue Lifecycle State

**Endpoint:** `PATCH /api/v1/issues/:issue_id`

**Headers:**
```http
X-SigTrap-Project-Key: <uuid>
Content-Type: application/json
```

**Request Payload:**
```json
{
  "status": "resolved"
}
```
*(Valid statuses: `unresolved`, `resolved`, `ignored`)*

**Response (200 OK):**
```json
{
  "issue_id": "grp_98234",
  "status": "resolved",
  "updated_at": 1723827500000
}
```

---

### 4.3 AI Root-Cause Diagnostic & Frame Code Context

Returns post-mortem analysis, source-mapped stack traces, and proposed code patches.

**Endpoint:** `GET /api/v1/issues/:issue_id/diagnostic`

**Response (200 OK):**
```json
{
  "issue_id": "grp_98234",
  "ai_analysis": {
    "status": "completed",
    "root_cause_summary": "The 'items' prop is undefined when the API returns a 500 error, causing the .map() function to crash during render.",
    "suggested_patch": "diff\n--- a/src/components/List.tsx\n+++ b/src/components/List.tsx\n@@ -45,3 +45,3 @@\n-      {items.map(item => (\n+      {items?.map(item => (\n",
    "confidence_score": 0.92,
    "generated_at": 1723827485000
  },
  "latest_event": {
    "event_id": "123e4567-e89b-12d3-a456-426614174000",
    "timestamp": 1723827484000,
    "environment": "production",
    "release_version": "v1.4.2-ab89c2",
    "unminified_stacktrace": [
      {
        "filename": "src/components/List.tsx",
        "function": "renderList",
        "lineno": 45,
        "colno": 12,
        "code_context": [
          "  return (",
          "    <div className='list-wrapper'>",
          "      {items.map(item => (",
          "        <ListItem key={item.id} {...item} />"
        ]
      }
    ],
    "breadcrumbs": [
      {
        "timestamp": 1723827481000,
        "category": "ui.click",
        "message": "div#submit-button.btn-primary",
        "data": {}
      },
      {
        "timestamp": 1723827482500,
        "category": "network.fetch",
        "message": "POST /api/checkout",
        "data": {
          "status_code": 500,
          "latency_ms": 150
        }
      }
    ],
    "context": {
      "browser": "Chrome 115.0.0.0",
      "os": "Windows 11",
      "url": "https://segv.tech/checkout",
      "viewport": {
        "width": 1920,
        "height": 1080
      }
    }
  }
}
```

---

### 4.4 Trigger AI Re-Diagnostic

Re-runs the AI Diagnostic Engine against the latest telemetry and source map diffs.

**Endpoint:** `POST /api/v1/issues/:issue_id/diagnostic/reanalyze`

**Response (200 OK):**
```json
{
  "issue_id": "grp_98234",
  "status": "processing",
  "message": "AI root-cause diagnostic re-analysis enqueued."
}
```

---

## 5. Operations & Health Endpoint

**Endpoint:** `GET /api/v1/health`

**Response (200 OK):**
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

---

## 6. Strict Type-Safety & Data Contract Guarantees

1. **Numeric Precision**: Timestamps (`timestamp`, `last_seen`, `first_seen`), coordinates (`lineno`, `colno`, `viewport.width`, `viewport.height`), and metrics (`event_count`, `users_affected`, `confidence_score`) **MUST** be numeric types in JSON payload schemas. Stringified numbers (e.g. `"45"`) are rejected with `400 Bad Request`.
2. **Ring Buffer Integrity**: `breadcrumbs` array elements beyond index 49 will be automatically truncated server-side.
3. **Exact Release Version Alignment**: `release_version` in `POST /api/v1/trap` must strictly match the `release_version` uploaded to `POST /api/v1/artifacts/sourcemaps` for unminification to match.
