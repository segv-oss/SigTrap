# ⚡ SIGTRAP

> **Catch the signal. Replay the state. Patch the root cause.**

`SIGTRAP` is a developer-first runtime telemetry and post-mortem debugging platform. Built by team **SEGV**, it hooks into web applications to trap unhandled runtime exceptions, record pre-crash state breadcrumbs, and run AI-assisted root-cause diagnosis directly against your codebase diffs.

---

## 💥 The Problem

Standard error logs are dead text. When an unhandled exception hits production, developers receive an unformatted stack trace with zero context:
* No record of the user interactions preceding the crash.
* No snapshot of active state or environment variables.
* Endless back-and-forth trying to reproduce edge cases locally.

## 🛡️ What SigTrap Does

`SIGTRAP` treats every client-side crash like an OS breakpoint trap:

- **Automated Trap Ingestion:** Zero-dependency SDK catches unhandled rejections, React error boundary crashes, and network failures in real time.
- **State & Breadcrumb Replay:** Records user clicks, navigation events, and mutation dispatches in a ring buffer prior to failure.
- **AI Root-Cause Diffing:** Parses the stack frame, maps it to the exact source lines, and runs an LLM diagnostic to generate an immediate patch.
- **Real-Time Cockpit:** A high-density dashboard built for engineers—inspect frame registers, payload history, and error velocity.

---

## 🏗️ Architecture

```text
  [ Client Web App ]
         │ (Trap Hook / Error Boundary)
         ▼
┌──────────────────┐      WebSocket / HTTP      ┌─────────────────────┐
│  SigTrap Browser │ ─────────────────────────► │   Go Ingestion Node │
│       SDK        │                            │  (Ring Buffer Core) │
└──────────────────┘                            └──────────┬──────────┘
                                                           │
                                ┌──────────────────────────┴──────────┐
                                ▼                                     ▼
                     ┌─────────────────────┐               ┌─────────────────────┐
                     │ PostgreSQL / ClickH │               │ AI Diagnostic Engine│
                     │ (Telemetry Storage) │               │ (Root-Cause + Patch)│
                     └──────────┬──────────┘               └──────────┬──────────┘
                                │                                     │
                                └──────────────────┬──────────────────┘
                                                   ▼
                                        ┌─────────────────────┐
                                        │  SigTrap React UI   │
                                        │ (Debugging Cockpit) │
                                        └─────────────────────┘

---

## 🚀 Getting Started

### Prerequisites

| Tool | Version |
|---|---|
| Go | 1.21+ |
| Node.js | 18+ |
| npm | 9+ |

### 1. Clone & configure

```bash
git clone https://github.com/your-org/SigTrap.git
cd SigTrap

# Copy the example env file and fill in your values
cp .env.example .env
```

Open `.env` and set at minimum:
```env
SIGTRAP_ADMIN_TOKEN=your-long-random-secret
SIGTRAP_PORT=8080
```

### 2. Install dependencies

```bash
make install
```

### 3. Run everything together

```bash
make dev
```

- **Backend API** → `http://localhost:8080`
- **Cockpit UI**  → `http://localhost:5173`

### 4. Fire a test crash

Open the cockpit and click **💥 Fire Test Trap** (top-right).  
A simulated `TypeError` is captured, AI-diagnosed, and appears in the sidebar within ~4 seconds.

---

### Optional: Upload a Source Map

```bash
curl -X POST http://localhost:8080/api/v1/artifacts/sourcemaps \
  -H "Authorization: Bearer your-long-random-secret" \
  -F "release_version=v1.4.2-ab89c2" \
  -F "file=@dist/assets/main.min.js.map"
```

> The `release_version` must exactly match the version the SDK was initialised with.

---

## 🗂️ Project Structure

```
SigTrap/
├── backend/
│   ├── cmd/server/         # Entry point
│   └── internal/
│       ├── ai/             # Gemini AI + heuristic engine
│       ├── handler/        # HTTP route handlers
│       ├── ingest/         # Async pipeline (worker pool)
│       ├── model/          # Shared data models
│       ├── sourcemap/      # VLQ decoder + source map manager
│       └── store/          # In-memory store + disk persistence
├── frontend/
│   └── src/
│       ├── sdk/            # Browser SDK (ringBuffer.ts, sigtrap.ts)
│       ├── types/          # Shared TypeScript interfaces
│       └── App.tsx         # Cockpit UI
├── .env.example
├── API_CONTRACT.md
└── Makefile
```
