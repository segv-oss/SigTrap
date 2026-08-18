import { useEffect, useState, useCallback } from 'react';
import { SigTrap } from './sdk/sigtrap';
import './App.css';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------
interface Issue {
  issue_id: string;
  type: string;
  value: string;
  culprit_file: string;
  event_count: number;
  users_affected: number;
  last_seen: number;
  first_seen: number;
  status: 'unresolved' | 'resolved' | 'ignored';
  release_version: string;
  project_id: string;
  environment: string;
}

interface StackFrame {
  filename: string;
  function?: string;
  lineno: number;
  colno: number;
  code_context?: string[];
}

interface Breadcrumb {
  timestamp: number;
  category: string;
  message: string;
  data?: Record<string, unknown>;
}

interface DiagnosticResponse {
  issue_id: string;
  ai_analysis: {
    status: string;
    root_cause_summary: string;
    suggested_patch: string;
    confidence_score: number;
    generated_at?: number;
  };
  latest_event: {
    event_id: string;
    timestamp: number;
    environment: string;
    release_version: string;
    unminified_stacktrace: StackFrame[];
    breadcrumbs: Breadcrumb[];
    context: {
      browser: string;
      os: string;
      url: string;
      viewport: { width: number; height: number };
    };
  };
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------
const API_BASE    = 'http://localhost:8080/api/v1';
const PROJECT_KEY = '123e4567-e89b-12d3-a456-426614174000';
const POLL_MS     = 4000;

// ---------------------------------------------------------------------------
// Utility helpers
// ---------------------------------------------------------------------------
function relativeTime(ms: number): string {
  const diff = Date.now() - ms;
  if (diff < 60_000)   return `${Math.floor(diff / 1000)}s ago`;
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return new Date(ms).toLocaleDateString();
}

function breadcrumbCategoryClass(cat: string): string {
  const map: Record<string, string> = {
    'ui.click':      'breadcrumb-row__cat--ui\\.click',
    'network.fetch': 'breadcrumb-row__cat--network\\.fetch',
    'navigation':    'breadcrumb-row__cat--navigation',
    'console.error': 'breadcrumb-row__cat--console\\.error',
  };
  return map[cat] ?? 'breadcrumb-row__cat--default';
}

// ---------------------------------------------------------------------------
// Mini sparkline (pure SVG, no library)
// ---------------------------------------------------------------------------
function Sparkline({ issue }: { issue: Issue }) {
  const W = 120, H = 20;

  // Build 8 synthetic data points decaying backwards from event_count
  const points: number[] = [];
  let v = issue.event_count;
  for (let i = 7; i >= 0; i--) {
    points[i] = Math.max(1, Math.round(v));
    v = v * (0.6 + Math.random() * 0.3);
  }

  const max = Math.max(...points, 1);
  const coords = points.map((p, i) => {
    const x = (i / (points.length - 1)) * W;
    const y = H - (p / max) * (H - 2) - 1;
    return `${x.toFixed(1)},${y.toFixed(1)}`;
  });

  return (
    <svg
      className="sparkline"
      width={W}
      height={H}
      viewBox={`0 0 ${W} ${H}`}
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
    >
      <polyline
        points={coords.join(' ')}
        stroke="var(--red)"
        strokeWidth="1.5"
        strokeLinejoin="round"
        strokeLinecap="round"
      />
    </svg>
  );
}

// ---------------------------------------------------------------------------
// IssueCard
// ---------------------------------------------------------------------------
interface IssueCardProps {
  issue: Issue;
  active: boolean;
  onClick: () => void;
}

function IssueCard({ issue, active, onClick }: IssueCardProps) {
  const statusClass =
    issue.status === 'resolved'
      ? 'issue-card__status--resolved'
      : issue.status === 'ignored'
      ? 'issue-card__status--ignored'
      : 'issue-card__status--unresolved';

  return (
    <div
      className={`issue-card${active ? ' issue-card--active' : ''}`}
      onClick={onClick}
      role="button"
      tabIndex={0}
      onKeyDown={(e) => e.key === 'Enter' && onClick()}
    >
      <div className="issue-card__header">
        <span className="issue-card__type">{issue.type}</span>
        <span className="issue-card__time">{relativeTime(issue.last_seen)}</span>
      </div>
      <div className="issue-card__message" title={issue.value}>{issue.value}</div>
      <div className="issue-card__file" title={issue.culprit_file}>
        {issue.culprit_file}
      </div>
      <div className="issue-card__meta">
        <span>🔥 <b>{issue.event_count}</b></span>
        <span>👤 <b>{issue.users_affected}</b></span>
        <span className={`issue-card__status ${statusClass}`}>{issue.status}</span>
      </div>
      {issue.event_count > 1 && <Sparkline issue={issue} />}
    </div>
  );
}

// ---------------------------------------------------------------------------
// StackTraceViewer
// ---------------------------------------------------------------------------
function StackTraceViewer({ frames }: { frames: StackFrame[] }) {
  if (!frames || frames.length === 0) {
    return (
      <p style={{ color: 'var(--text-faint)', fontSize: 12, padding: '8px 0' }}>
        No stack frames available.
      </p>
    );
  }

  return (
    <>
      {frames.map((frame, idx) => (
        <div key={idx} className="stack-frame">
          <div className="stack-frame__header">
            <span>
              <span className="stack-frame__file">{frame.filename}</span>
              {frame.function && (
                <span className="stack-frame__fn"> in {frame.function}</span>
              )}
            </span>
            <span className="stack-frame__loc">
              L{frame.lineno}:{frame.colno}
            </span>
          </div>
          {frame.code_context && frame.code_context.length > 0 && (
            <div className="code-context">
              {frame.code_context.map((line, lIdx) => {
                const isHot = line.includes('.map') || line.includes('undefined');
                return (
                  <div
                    key={lIdx}
                    className={`code-line${isHot ? ' code-line--highlight' : ''}`}
                  >
                    <span className="code-line__num">{lIdx + 1}</span>
                    <span className="code-line__text">{line}</span>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      ))}
    </>
  );
}

// ---------------------------------------------------------------------------
// BreadcrumbTimeline
// ---------------------------------------------------------------------------
function BreadcrumbTimeline({ breadcrumbs }: { breadcrumbs: Breadcrumb[] }) {
  if (!breadcrumbs || breadcrumbs.length === 0) {
    return (
      <p style={{ color: 'var(--text-faint)', fontSize: 12, padding: '8px 0' }}>
        No breadcrumbs captured.
      </p>
    );
  }

  return (
    <>
      {breadcrumbs.map((b, idx) => (
        <div key={idx} className="breadcrumb-row">
          <span className="breadcrumb-row__time">
            {new Date(b.timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })}
          </span>
          <span className={`breadcrumb-row__cat ${breadcrumbCategoryClass(b.category)}`}>
            {b.category}
          </span>
          <span className="breadcrumb-row__msg">{b.message}</span>
          {b.data && Object.keys(b.data).length > 0 && (
            <span className="breadcrumb-row__data">
              {JSON.stringify(b.data)}
            </span>
          )}
        </div>
      ))}
    </>
  );
}

// ---------------------------------------------------------------------------
// EnvironmentRegisters
// ---------------------------------------------------------------------------
function EnvironmentRegisters({ ctx }: { ctx: DiagnosticResponse['latest_event']['context'] }) {
  const registers = [
    { label: 'Browser', value: ctx.browser },
    { label: 'OS',      value: ctx.os },
    { label: 'Viewport', value: `${ctx.viewport.width} × ${ctx.viewport.height}` },
    { label: 'Crash URL', value: ctx.url },
  ];

  return (
    <div className="registers-grid">
      {registers.map((r) => (
        <div key={r.label} className="register">
          <div className="register__label">{r.label}</div>
          <div className="register__value" title={r.value}>{r.value}</div>
        </div>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// DiagnosticPanel
// ---------------------------------------------------------------------------
interface DiagnosticPanelProps {
  diagnostic: DiagnosticResponse;
  loading: boolean;
  onReanalyze: () => void;
  onResolve: () => void;
  onIgnore: () => void;
}

function DiagnosticPanel({
  diagnostic,
  loading,
  onReanalyze,
  onResolve,
  onIgnore,
}: DiagnosticPanelProps) {
  const [copied, setCopied] = useState(false);

  const copyPatch = () => {
    navigator.clipboard.writeText(diagnostic.ai_analysis.suggested_patch);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const evt = diagnostic.latest_event;
  const ai  = diagnostic.ai_analysis;

  return (
    <div className="panel__stack">

      {/* Issue header */}
      <div className="card card__body">
        <div className="issue-header">
          <div>
            <div className="issue-header__meta">
              <span className="issue-header__id">{diagnostic.issue_id}</span>
              <span className="pill">{evt.environment}</span>
              <span className="pill">{evt.release_version}</span>
            </div>
            <h2 className="issue-header__title">
              {evt.unminified_stacktrace[0]?.filename ?? 'Crash Exception'}
            </h2>
          </div>
          <div className="issue-header__actions">
            <button
              id="btn-reanalyze"
              className="btn btn--ghost"
              onClick={onReanalyze}
              disabled={loading}
            >
              {loading ? <><span className="spinner" /> Analyzing…</> : '↻ Re-run AI'}
            </button>
            <button
              id="btn-resolve"
              className="btn btn--success"
              onClick={onResolve}
            >
              ✓ Resolve
            </button>
            <button
              id="btn-ignore"
              className="btn btn--ghost"
              onClick={onIgnore}
            >
              Ignore
            </button>
          </div>
        </div>
      </div>

      {/* AI Root-cause card */}
      <div className="ai-card">
        <div className="ai-card__header">
          <div className="ai-card__label">
            <span>🤖</span>
            <span>AI Root-Cause Diagnosis</span>
          </div>
          <span className="confidence-badge">
            {(ai.confidence_score * 100).toFixed(0)}% confidence
          </span>
        </div>
        <div className="ai-card__body">
          <p className="ai-card__summary">{ai.root_cause_summary}</p>
          <div className="diff-block">
            <div className="diff-block__toolbar">
              <span className="diff-block__label">Suggested patch</span>
              <button
                id="btn-copy-patch"
                className="btn btn--ghost btn--sm"
                onClick={copyPatch}
              >
                {copied ? '✓ Copied' : '⎘ Copy diff'}
              </button>
            </div>
            <pre className="diff-block__pre">{ai.suggested_patch}</pre>
          </div>
        </div>
      </div>

      {/* Stack trace */}
      <div className="card">
        <div className="card__header">
          <span className="card__title">
            <span>{'</>'}</span> Unminified Stack Trace
          </span>
          <span style={{ fontSize: 11, color: 'var(--text-faint)' }}>
            {evt.unminified_stacktrace.length} frames
          </span>
        </div>
        <div className="card__body">
          <StackTraceViewer frames={evt.unminified_stacktrace} />
        </div>
      </div>

      {/* Breadcrumb replay */}
      <div className="card">
        <div className="card__header">
          <span className="card__title">
            <span>⏱</span> Breadcrumb Ring Buffer Replay
          </span>
          <span style={{ fontSize: 11, color: 'var(--text-faint)' }}>
            {evt.breadcrumbs.length} / 50 events
          </span>
        </div>
        <div className="card__body">
          <BreadcrumbTimeline breadcrumbs={evt.breadcrumbs} />
        </div>
      </div>

      {/* Environment registers */}
      <div className="card">
        <div className="card__header">
          <span className="card__title">
            <span>🌐</span> Runtime Environment
          </span>
        </div>
        <div className="card__body">
          <EnvironmentRegisters ctx={evt.context} />
        </div>
      </div>

    </div>
  );
}

// ---------------------------------------------------------------------------
// App (root)
// ---------------------------------------------------------------------------
export function App() {
  const [issues, setIssues]               = useState<Issue[]>([]);
  const [selectedID, setSelectedID]       = useState<string | null>(null);
  const [diagnostic, setDiagnostic]       = useState<DiagnosticResponse | null>(null);
  const [loadingIssues, setLoadingIssues] = useState(true);
  const [loadingDiag, setLoadingDiag]     = useState(false);
  const [statusFilter, setStatusFilter]   = useState<string>('unresolved');
  const [envFilter, setEnvFilter]         = useState<string>('production');
  const [search, setSearch]               = useState<string>('');
  const [backendOnline, setBackendOnline] = useState(true);

  // Init SDK
  useEffect(() => {
    SigTrap.init({
      projectKey:     PROJECT_KEY,
      endpoint:       `${API_BASE}/trap`,
      environment:    'production',
      releaseVersion: 'v1.4.2-ab89c2',
    });
  }, []);

  // Fetch issues (with polling)
  const fetchIssues = useCallback(async () => {
    setLoadingIssues(true);
    try {
      const params = new URLSearchParams({
        project_id: PROJECT_KEY,
        env:        envFilter,
        status:     statusFilter,
        search:     search,
      });
      const res = await fetch(`${API_BASE}/issues?${params}`);
      if (res.ok) {
        const data = await res.json();
        const list: Issue[] = data.issues ?? [];
        setIssues(list);
        setBackendOnline(true);
        if (list.length > 0 && !selectedID) {
          setSelectedID(list[0].issue_id);
        }
      } else {
        setBackendOnline(false);
      }
    } catch {
      setBackendOnline(false);
    } finally {
      setLoadingIssues(false);
    }
  }, [envFilter, statusFilter, search, selectedID]);

  useEffect(() => {
    fetchIssues();
    const id = setInterval(fetchIssues, POLL_MS);
    return () => clearInterval(id);
  }, [fetchIssues]);

  // Fetch diagnostic when issue selected
  useEffect(() => {
    if (!selectedID) return;
    setLoadingDiag(true);
    fetch(`${API_BASE}/issues/${selectedID}/diagnostic`)
      .then((r) => r.ok ? r.json() : null)
      .then((data) => { if (data) setDiagnostic(data); })
      .catch(console.error)
      .finally(() => setLoadingDiag(false));
  }, [selectedID]);

  const updateStatus = async (id: string, status: string) => {
    await fetch(`${API_BASE}/issues/${id}`, {
      method: 'PATCH',
      headers: {
        'Content-Type': 'application/json',
        'X-SigTrap-Project-Key': PROJECT_KEY,
      },
      body: JSON.stringify({ status }),
    });
    fetchIssues();
  };

  const triggerReanalyze = async () => {
    if (!selectedID) return;
    setLoadingDiag(true);
    try {
      await fetch(`${API_BASE}/issues/${selectedID}/diagnostic/reanalyze`, { method: 'POST' });
      const r = await fetch(`${API_BASE}/issues/${selectedID}/diagnostic`);
      if (r.ok) setDiagnostic(await r.json());
    } catch (e) {
      console.error(e);
    } finally {
      setLoadingDiag(false);
    }
  };

  const simulateCrash = () => {
    SigTrap.addBreadcrumb({ category: 'ui.click', message: 'button#fire-test-trap' });
    SigTrap.addBreadcrumb({
      category: 'network.fetch',
      message: 'POST /api/checkout',
      data: { status_code: 500, latency_ms: 210 },
    });
    const err = new TypeError("Cannot read properties of undefined (reading 'map')");
    err.stack = [
      "TypeError: Cannot read properties of undefined (reading 'map')",
      '    at renderList (https://segv.tech/assets/main.min.js:1:4892)',
      '    at App (https://segv.tech/assets/main.min.js:1:1200)',
    ].join('\n');
    SigTrap.captureException(err, 'TypeError');
    setTimeout(fetchIssues, 600);
  };

  return (
    <div className="app-shell">
      {/* ── Top navigation ─────────────────────────────────────────────── */}
      <header className="topnav">
        <div className="topnav__brand">
          <span className="topnav__logo">⚡</span>
          <div>
            <div style={{ display: 'flex', alignItems: 'center' }}>
              <span className="topnav__title">SIGTRAP</span>
              <span className="topnav__badge">v1.1 COCKPIT</span>
            </div>
            <div className="topnav__subtitle">
              Catch the signal. Replay the state. Patch the root cause.
            </div>
          </div>
        </div>

        <div className="topnav__controls">
          <div className={`status-dot status-dot--${backendOnline ? 'online' : 'offline'}`}>
            <span className="status-dot__indicator" />
            {backendOnline ? 'Backend online' : 'Backend offline'}
          </div>

          <select
            id="env-select"
            className="select"
            value={envFilter}
            onChange={(e) => setEnvFilter(e.target.value)}
          >
            <option value="production">production</option>
            <option value="staging">staging</option>
            <option value="all">all envs</option>
          </select>

          <button
            id="btn-fire-test-trap"
            className="btn btn--danger"
            onClick={simulateCrash}
          >
            💥 Fire Test Trap
          </button>
        </div>
      </header>

      {/* ── Body ───────────────────────────────────────────────────────── */}
      <div className="app-body">

        {/* Sidebar */}
        <aside className="sidebar">
          <div className="sidebar__filters">
            <input
              id="search-input"
              className="input"
              type="text"
              placeholder="Search by exception, message, file…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            <div className="sidebar__tabs">
              {(['unresolved', 'resolved', 'ignored', 'all'] as const).map((s) => (
                <button
                  key={s}
                  id={`tab-${s}`}
                  className={`sidebar__tab${statusFilter === s ? ' sidebar__tab--active' : ''}`}
                  onClick={() => setStatusFilter(s)}
                >
                  {s}
                </button>
              ))}
            </div>
          </div>

          <div className="sidebar__list">
            {loadingIssues && issues.length === 0 ? (
              <div className="sidebar__empty">
                <span className="spinner" style={{ margin: '0 auto 8px' }} />
                Loading issues…
              </div>
            ) : issues.length === 0 ? (
              <div className="sidebar__empty">
                No issues found.<br />
                Click <strong>Fire Test Trap</strong> to capture a live crash.
              </div>
            ) : (
              issues.map((issue) => (
                <IssueCard
                  key={issue.issue_id}
                  issue={issue}
                  active={issue.issue_id === selectedID}
                  onClick={() => {
                    setSelectedID(issue.issue_id);
                    setDiagnostic(null);
                  }}
                />
              ))
            )}
          </div>
        </aside>

        {/* Main panel */}
        <main className="panel">
          {loadingDiag ? (
            <div className="panel__loading">
              <span className="spinner" />
              Running AI root-cause diagnostic…
            </div>
          ) : !diagnostic ? (
            <div className="panel__empty">
              Select an issue to inspect the AI diagnosis & stack trace.
            </div>
          ) : (
            <DiagnosticPanel
              diagnostic={diagnostic}
              loading={loadingDiag}
              onReanalyze={triggerReanalyze}
              onResolve={() => updateStatus(diagnostic.issue_id, 'resolved')}
              onIgnore={() => updateStatus(diagnostic.issue_id, 'ignored')}
            />
          )}
        </main>

      </div>
    </div>
  );
}

export default App;
