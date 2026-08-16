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
