import React, { useEffect, useState } from 'react';
import { SigTrap } from './sdk/sigtrap';
import './App.css';

interface Issue {
  issue_id: string;
  type: string;
  value: string;
  culprit_file: string;
  event_count: number;
  users_affected: number;
  last_seen: number;
  first_seen: number;
  status: string;
  release_version: string;
  project_id: string;
  environment: string;
}

interface StackFrame {
  filename: string;
  function: string;
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
      viewport: {
        width: number;
        height: number;
      };
    };
  };
}

const API_BASE = 'http://localhost:8080/api/v1';
const PROJECT_KEY = '123e4567-e89b-12d3-a456-426614174000';

export function App() {
  const [issues, setIssues] = useState<Issue[]>([]);
  const [selectedIssueID, setSelectedIssueID] = useState<string | null>(null);
  const [diagnostic, setDiagnostic] = useState<DiagnosticResponse | null>(null);
  const [loadingIssues, setLoadingIssues] = useState<boolean>(true);
  const [loadingDiagnostic, setLoadingDiagnostic] = useState<boolean>(false);
  const [statusFilter, setStatusFilter] = useState<string>('unresolved');
  const [envFilter, setEnvFilter] = useState<string>('production');
  const [searchQuery, setSearchQuery] = useState<string>('');
  const [copiedPatch, setCopiedPatch] = useState<boolean>(false);
  const [backendOnline, setBackendOnline] = useState<boolean>(true);

  // Initialize SigTrap SDK on load
  useEffect(() => {
    SigTrap.init({
      projectKey: PROJECT_KEY,
      endpoint: `${API_BASE}/trap`,
      environment: 'production',
      releaseVersion: 'v1.4.2-ab89c2',
    });
  }, []);

  // Fetch Issues List
  const fetchIssues = async () => {
    setLoadingIssues(true);
    try {
      const url = `${API_BASE}/issues?project_id=${PROJECT_KEY}&env=${envFilter}&status=${statusFilter}&search=${encodeURIComponent(searchQuery)}`;
      const res = await fetch(url);
      if (res.ok) {
        const data = await res.json();
        setIssues(data.issues || []);
        setBackendOnline(true);
        if (data.issues && data.issues.length > 0 && !selectedIssueID) {
          setSelectedIssueID(data.issues[0].issue_id);
        }
      } else {
        setBackendOnline(false);
      }
    } catch {
      setBackendOnline(false);
    } finally {
      setLoadingIssues(false);
    }
  };

  useEffect(() => {
    fetchIssues();
    const interval = setInterval(fetchIssues, 4000); // Polling every 4s
    return () => clearInterval(interval);
  }, [statusFilter, envFilter, searchQuery]);

  // Fetch Issue Diagnostic Details
  useEffect(() => {
    if (!selectedIssueID) return;

    const fetchDiagnostic = async () => {
      setLoadingDiagnostic(true);
      try {
        const res = await fetch(`${API_BASE}/issues/${selectedIssueID}/diagnostic`);
        if (res.ok) {
          const data = await res.json();
          setDiagnostic(data);
        }
      } catch (err) {
        console.error('Failed to fetch diagnostic:', err);
      } finally {
        setLoadingDiagnostic(false);
      }
    };

    fetchDiagnostic();
  }, [selectedIssueID]);

  // Update Issue Status (Resolve / Ignore)
  const updateStatus = async (issueID: string, newStatus: string) => {
    try {
      const res = await fetch(`${API_BASE}/issues/${issueID}`, {
        method: 'PATCH',
        headers: {
          'Content-Type': 'application/json',
          'X-SigTrap-Project-Key': PROJECT_KEY,
        },
        body: JSON.stringify({ status: newStatus }),
      });
      if (res.ok) {
        fetchIssues();
      }
    } catch (err) {
      console.error('Failed to update issue status:', err);
    }
  };

  // Trigger Fresh AI Diagnostic Reanalysis
  const triggerReanalyze = async (issueID: string) => {
    setLoadingDiagnostic(true);
    try {
      const res = await fetch(`${API_BASE}/issues/${issueID}/diagnostic/reanalyze`, {
        method: 'POST',
      });
      if (res.ok) {
        const resDiag = await fetch(`${API_BASE}/issues/${issueID}/diagnostic`);
        if (resDiag.ok) {
          const data = await resDiag.json();
          setDiagnostic(data);
        }
      }
    } catch (err) {
      console.error('Reanalyze failed:', err);
    } finally {
      setLoadingDiagnostic(false);
    }
  };

  // Simulate a live client-side crash to test telemetry ingestion
  const simulateLiveCrash = async () => {
    SigTrap.addBreadcrumb({
      category: 'ui.click',
      message: 'button#checkout-submit.btn-primary',
    });
    SigTrap.addBreadcrumb({
      category: 'network.fetch',
      message: 'POST /api/checkout',
      data: { status_code: 500, latency_ms: 180 },
    });

    // Fire simulated TypeError crash
    const fakeError = new TypeError("Cannot read properties of undefined (reading 'map')");
    fakeError.stack = "TypeError: Cannot read properties of undefined (reading 'map')\n    at renderList (https://segv.tech/assets/main.min.js:1:4892)";
    
    SigTrap.captureException(fakeError, 'TypeError');

    setTimeout(() => {
      fetchIssues();
    }, 500);
  };

  const copyPatchToClipboard = (patchText: string) => {
    navigator.clipboard.writeText(patchText);
    setCopiedPatch(true);
    setTimeout(() => setCopiedPatch(false), 2000);
  };

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100vh', backgroundColor: '#0a0c10', color: '#e6edf3', fontFamily: 'Inter, sans-serif' }}>
      
      {/* ⚡ TOP NAVIGATION HEADER */}
      <header style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '12px 24px', backgroundColor: '#0d1017', borderBottom: '1px solid #21283b' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
          <span style={{ fontSize: '24px' }}>⚡</span>
          <div>
            <h1 style={{ margin: 0, fontSize: '18px', fontWeight: 800, letterSpacing: '0.5px', color: '#ffffff' }}>
              SIGTRAP <span style={{ fontSize: '11px', fontWeight: 600, padding: '2px 8px', borderRadius: '12px', backgroundColor: 'rgba(0, 240, 255, 0.15)', color: '#00f0ff', border: '1px solid rgba(0,240,255,0.3)', marginLeft: '8px' }}>v1.1 COCKPIT</span>
            </h1>
            <p style={{ margin: 0, fontSize: '11px', color: '#8b949e' }}>Catch the signal. Replay the state. Patch the root cause.</p>
          </div>
        </div>

        <div style={{ display: 'flex', alignItems: 'center', gap: '16px' }}>
          {/* Status Indicator */}
          <div style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '12px', color: backendOnline ? '#2ea043' : '#f85149' }}>
            <span style={{ width: '8px', height: '8px', borderRadius: '50%', backgroundColor: backendOnline ? '#2ea043' : '#f85149', display: 'inline-block' }}></span>
            {backendOnline ? 'Backend Online (Port 8080)' : 'Backend Disconnected'}
          </div>

          {/* Environment Switcher */}
          <select 
            value={envFilter} 
            onChange={(e) => setEnvFilter(e.target.value)}
            style={{ backgroundColor: '#12161f', color: '#e6edf3', border: '1px solid #21283b', borderRadius: '6px', padding: '6px 12px', fontSize: '12px' }}
          >
            <option value="production">Env: production</option>
            <option value="staging">Env: staging</option>
            <option value="all">Env: all</option>
          </select>

          {/* Trigger Crash Test Button */}
          <button
            onClick={simulateLiveCrash}
            className="btn-interactive"
            style={{ backgroundColor: '#f85149', color: '#ffffff', border: 'none', borderRadius: '6px', padding: '6px 14px', fontSize: '12px', fontWeight: 700, cursor: 'pointer', display: 'flex', alignItems: 'center', gap: '6px' }}
          >
            💥 Fire Test Trap
          </button>
        </div>
      </header>

      {/* 🚀 MAIN DASHBOARD CONTENT AREA */}
      <div style={{ display: 'flex', flex: 1, overflow: 'hidden' }}>

        {/* 📋 LEFT SIDEBAR: AGGREGATED ISSUES LIST */}
        <div style={{ width: '380px', backgroundColor: '#0d1017', borderRight: '1px solid #21283b', display: 'flex', flexDirection: 'column' }}>
          
          {/* Filter Bar */}
          <div style={{ padding: '16px', borderBottom: '1px solid #21283b', display: 'flex', flexDirection: 'column', gap: '12px' }}>
            <input
              type="text"
              placeholder="Search issues by exception, message, file..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              style={{ backgroundColor: '#12161f', color: '#e6edf3', border: '1px solid #21283b', borderRadius: '6px', padding: '8px 12px', fontSize: '12px', width: '100%', boxSizing: 'border-box' }}
            />
            <div style={{ display: 'flex', gap: '4px', backgroundColor: '#12161f', padding: '3px', borderRadius: '6px', border: '1px solid #21283b' }}>
              {['unresolved', 'resolved', 'ignored', 'all'].map((st) => (
                <button
                  key={st}
                  onClick={() => setStatusFilter(st)}
                  style={{
                    flex: 1,
                    padding: '4px 0',
                    fontSize: '11px',
                    fontWeight: 600,
                    borderRadius: '4px',
                    border: 'none',
                    backgroundColor: statusFilter === st ? '#21283b' : 'transparent',
                    color: statusFilter === st ? '#00f0ff' : '#8b949e',
                    cursor: 'pointer',
                    textTransform: 'capitalize',
                  }}
                >
                  {st}
                </button>
              ))}
            </div>
          </div>

          {/* Issues Scroll Area */}
          <div style={{ flex: 1, overflowY: 'auto', padding: '8px' }}>
            {loadingIssues && issues.length === 0 ? (
              <div style={{ padding: '32px', textAlign: 'center', color: '#8b949e', fontSize: '13px' }}>Loading issues...</div>
            ) : issues.length === 0 ? (
              <div style={{ padding: '32px', textAlign: 'center', color: '#8b949e', fontSize: '13px' }}>
                No issues found matching criteria. Click <b>"Fire Test Trap"</b> to capture a live crash!
              </div>
            ) : (
              issues.map((issue) => {
                const isSelected = issue.issue_id === selectedIssueID;
                return (
                  <div
                    key={issue.issue_id}
                    onClick={() => setSelectedIssueID(issue.issue_id)}
                    className="card-glow"
                    style={{
                      padding: '12px',
                      borderRadius: '8px',
                      backgroundColor: isSelected ? '#181e2b' : '#12161f',
                      border: isSelected ? '1px solid #00f0ff' : '1px solid #21283b',
                      marginBottom: '8px',
                      cursor: 'pointer',
                    }}
                  >
                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '6px' }}>
                      <span style={{ fontSize: '11px', fontWeight: 700, padding: '2px 6px', borderRadius: '4px', backgroundColor: 'rgba(248, 81, 73, 0.15)', color: '#f85149', fontFamily: 'JetBrains Mono, monospace' }}>
                        {issue.type}
                      </span>
                      <span style={{ fontSize: '11px', color: '#8b949e' }}>
                        {new Date(issue.last_seen).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}
                      </span>
                    </div>

                    <div style={{ fontSize: '13px', fontWeight: 600, color: '#e6edf3', marginBottom: '6px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                      {issue.value}
                    </div>

                    <div style={{ fontSize: '11px', color: '#00f0ff', fontFamily: 'JetBrains Mono, monospace', marginBottom: '8px', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>
                      📍 {issue.culprit_file}
                    </div>

                    <div style={{ display: 'flex', alignItems: 'center', gap: '12px', fontSize: '11px', color: '#8b949e' }}>
                      <span>🔥 <b>{issue.event_count}</b> events</span>
                      <span>👤 <b>{issue.users_affected}</b> user</span>
                      <span style={{ marginLeft: 'auto', textTransform: 'capitalize', color: issue.status === 'resolved' ? '#2ea043' : '#d29922' }}>
                        ● {issue.status}
                      </span>
                    </div>
                  </div>
                );
              })
            )}
          </div>
        </div>

        {/* 🔬 RIGHT PANEL: POST-MORTEM DIAGNOSTIC & STACKTRACE REPLAY */}
        <div style={{ flex: 1, backgroundColor: '#0a0c10', display: 'flex', flexDirection: 'column', overflowY: 'auto', padding: '24px' }}>
          {loadingDiagnostic ? (
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: '#00f0ff', fontSize: '14px' }}>
              ⚡ Running AI Root-Cause Diagnostic Analysis...
            </div>
          ) : !diagnostic ? (
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', height: '100%', color: '#8b949e', fontSize: '14px' }}>
              Select an issue from the list to inspect frame registers & AI patches.
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '20px' }}>
              
              {/* ISSUE HEADER BAR */}
              <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', backgroundColor: '#12161f', padding: '16px', borderRadius: '8px', border: '1px solid #21283b' }}>
                <div>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '4px' }}>
                    <span style={{ fontSize: '12px', color: '#8b949e', fontFamily: 'JetBrains Mono, monospace' }}>{diagnostic.issue_id}</span>
                    <span style={{ fontSize: '11px', padding: '2px 8px', borderRadius: '12px', backgroundColor: '#21283b', color: '#e6edf3' }}>
                      {diagnostic.latest_event.environment}
                    </span>
                    <span style={{ fontSize: '11px', padding: '2px 8px', borderRadius: '12px', backgroundColor: '#21283b', color: '#00f0ff', fontFamily: 'JetBrains Mono, monospace' }}>
                      {diagnostic.latest_event.release_version}
                    </span>
                  </div>
                  <h2 style={{ margin: 0, fontSize: '18px', color: '#ffffff', fontWeight: 700 }}>
                    {diagnostic.latest_event.unminified_stacktrace[0]?.filename || 'Crash Exception'}
                  </h2>
                </div>

                <div style={{ display: 'flex', gap: '8px' }}>
                  <button
                    onClick={() => triggerReanalyze(diagnostic.issue_id)}
                    className="btn-interactive"
                    style={{ backgroundColor: '#21283b', color: '#00f0ff', border: '1px solid rgba(0,240,255,0.3)', borderRadius: '6px', padding: '8px 14px', fontSize: '12px', fontWeight: 600, cursor: 'pointer' }}
                  >
                    🤖 Re-run AI Diagnosis
                  </button>
                  <button
                    onClick={() => updateStatus(diagnostic.issue_id, 'resolved')}
                    className="btn-interactive"
                    style={{ backgroundColor: '#2ea043', color: '#ffffff', border: 'none', borderRadius: '6px', padding: '8px 14px', fontSize: '12px', fontWeight: 700, cursor: 'pointer' }}
                  >
                    ✓ Resolve Issue
                  </button>
                  <button
                    onClick={() => updateStatus(diagnostic.issue_id, 'ignored')}
                    className="btn-interactive"
                    style={{ backgroundColor: '#21283b', color: '#8b949e', border: 'none', borderRadius: '6px', padding: '8px 14px', fontSize: '12px', fontWeight: 600, cursor: 'pointer' }}
                  >
                    Ignore
                  </button>
                </div>
              </div>

              {/* 🤖 AI ROOT CAUSE DIAGNOSTIC CARD */}
              <div style={{ backgroundColor: '#12161f', borderRadius: '8px', border: '1px solid rgba(0, 240, 255, 0.4)', padding: '20px', boxShadow: '0 0 20px rgba(0, 240, 255, 0.05)' }}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '12px' }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <span style={{ fontSize: '18px' }}>🤖</span>
                    <h3 style={{ margin: 0, fontSize: '15px', color: '#00f0ff', fontWeight: 700 }}>AI Root-Cause Diagnosis</h3>
                  </div>
                  <span style={{ fontSize: '11px', fontWeight: 700, padding: '3px 10px', borderRadius: '12px', backgroundColor: 'rgba(46, 160, 67, 0.15)', color: '#2ea043', border: '1px solid rgba(46, 160, 67, 0.3)' }}>
                    Confidence Score: {(diagnostic.ai_analysis.confidence_score * 100).toFixed(0)}%
                  </span>
                </div>

                <p style={{ margin: '0 0 16px 0', fontSize: '13px', lineHeight: '1.6', color: '#e6edf3' }}>
                  {diagnostic.ai_analysis.root_cause_summary}
                </p>

                {/* SUGGESTED PATCH DIFF BOX */}
                <div style={{ backgroundColor: '#0d1017', borderRadius: '6px', border: '1px solid #21283b', overflow: 'hidden' }}>
                  <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '8px 14px', backgroundColor: '#181e2b', borderBottom: '1px solid #21283b', fontSize: '12px', fontWeight: 600, color: '#8b949e' }}>
                    <span>Suggested Git Diff Patch</span>
                    <button
                      onClick={() => copyPatchToClipboard(diagnostic.ai_analysis.suggested_patch)}
                      style={{ backgroundColor: '#21283b', color: '#00f0ff', border: 'none', borderRadius: '4px', padding: '4px 10px', fontSize: '11px', cursor: 'pointer' }}
                    >
                      {copiedPatch ? '✓ Copied Diff!' : '📋 Copy Patch'}
                    </button>
                  </div>
                  <pre style={{ margin: 0, padding: '14px', fontSize: '12px', fontFamily: 'JetBrains Mono, monospace', color: '#2ea043', overflowX: 'auto' }}>
                    {diagnostic.ai_analysis.suggested_patch}
                  </pre>
                </div>
              </div>

              {/* 📜 SOURCE-MAPPED STACK TRACE & CODE CONTEXT */}
              <div style={{ backgroundColor: '#12161f', borderRadius: '8px', border: '1px solid #21283b', padding: '20px' }}>
                <h3 style={{ margin: '0 0 16px 0', fontSize: '14px', color: '#ffffff', fontWeight: 700 }}>Unminified Stack Frame & Code Context</h3>
                
                {diagnostic.latest_event.unminified_stacktrace.map((frame, idx) => (
                  <div key={idx} style={{ marginBottom: '16px', backgroundColor: '#0d1017', borderRadius: '6px', border: '1px solid #21283b', overflow: 'hidden' }}>
                    <div style={{ padding: '10px 14px', backgroundColor: '#181e2b', borderBottom: '1px solid #21283b', fontSize: '12px', fontFamily: 'JetBrains Mono, monospace', color: '#00f0ff', display: 'flex', justifyContent: 'space-between' }}>
                      <span>📄 <b>{frame.filename}</b> in <code>{frame.function || 'anonymous'}</code></span>
                      <span style={{ color: '#8b949e' }}>Line {frame.lineno}:{frame.colno}</span>
                    </div>

                    {frame.code_context && frame.code_context.length > 0 && (
                      <div style={{ padding: '12px', fontFamily: 'JetBrains Mono, monospace', fontSize: '12px', backgroundColor: '#0d1017' }}>
                        {frame.code_context.map((line, lIdx) => (
                          <div key={lIdx} style={{ display: 'flex', gap: '12px', color: line.includes('.map') ? '#f85149' : '#8b949e', backgroundColor: line.includes('.map') ? 'rgba(248, 81, 73, 0.1)' : 'transparent', padding: '2px 4px', borderRadius: '2px' }}>
                            <span style={{ width: '24px', textAlign: 'right', color: '#57606a', userSelect: 'none' }}>{lIdx + 1}</span>
                            <span style={{ color: line.includes('.map') ? '#f85149' : '#e6edf3' }}>{line}</span>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                ))}
              </div>

              {/* 📼 BREADCRUMB RING BUFFER REPLAY TIMELINE */}
              <div style={{ backgroundColor: '#12161f', borderRadius: '8px', border: '1px solid #21283b', padding: '20px' }}>
                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '16px' }}>
                  <h3 style={{ margin: 0, fontSize: '14px', color: '#ffffff', fontWeight: 700 }}>50-Event Breadcrumb Ring Buffer Replay</h3>
                  <span style={{ fontSize: '11px', color: '#8b949e' }}>{diagnostic.latest_event.breadcrumbs.length} captured events</span>
                </div>

                <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                  {diagnostic.latest_event.breadcrumbs.map((b, idx) => (
                    <div key={idx} style={{ display: 'flex', alignItems: 'center', gap: '12px', padding: '8px 12px', backgroundColor: '#0d1017', borderRadius: '6px', border: '1px solid #21283b', fontSize: '12px' }}>
                      <span style={{ fontSize: '10px', fontFamily: 'JetBrains Mono, monospace', color: '#57606a' }}>
                        {new Date(b.timestamp).toLocaleTimeString()}
                      </span>
                      <span style={{ fontSize: '10px', fontWeight: 700, padding: '2px 6px', borderRadius: '4px', backgroundColor: b.category === 'network.fetch' ? 'rgba(163, 113, 247, 0.15)' : 'rgba(0, 240, 255, 0.15)', color: b.category === 'network.fetch' ? '#a371f7' : '#00f0ff' }}>
                        {b.category}
                      </span>
                      <span style={{ color: '#e6edf3', fontFamily: 'JetBrains Mono, monospace' }}>{b.message}</span>
                      {b.data && (
                        <span style={{ marginLeft: 'auto', fontSize: '11px', color: '#8b949e', fontFamily: 'JetBrains Mono, monospace' }}>
                          {JSON.stringify(b.data)}
                        </span>
                      )}
                    </div>
                  ))}
                </div>
              </div>

              {/* 🌐 RUNTIME ENVIRONMENT REGISTERS */}
              <div style={{ backgroundColor: '#12161f', borderRadius: '8px', border: '1px solid #21283b', padding: '20px' }}>
                <h3 style={{ margin: '0 0 12px 0', fontSize: '14px', color: '#ffffff', fontWeight: 700 }}>Runtime Environment & Context Registers</h3>
                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(4, 1fr)', gap: '12px', fontSize: '12px' }}>
                  <div style={{ backgroundColor: '#0d1017', padding: '10px', borderRadius: '6px', border: '1px solid #21283b' }}>
                    <div style={{ color: '#8b949e', fontSize: '10px', marginBottom: '4px' }}>BROWSER</div>
                    <div style={{ fontWeight: 600, color: '#e6edf3' }}>{diagnostic.latest_event.context.browser}</div>
                  </div>
                  <div style={{ backgroundColor: '#0d1017', padding: '10px', borderRadius: '6px', border: '1px solid #21283b' }}>
                    <div style={{ color: '#8b949e', fontSize: '10px', marginBottom: '4px' }}>OS</div>
                    <div style={{ fontWeight: 600, color: '#e6edf3' }}>{diagnostic.latest_event.context.os}</div>
                  </div>
                  <div style={{ backgroundColor: '#0d1017', padding: '10px', borderRadius: '6px', border: '1px solid #21283b' }}>
                    <div style={{ color: '#8b949e', fontSize: '10px', marginBottom: '4px' }}>VIEWPORT</div>
                    <div style={{ fontWeight: 600, color: '#e6edf3' }}>{diagnostic.latest_event.context.viewport.width} x {diagnostic.latest_event.context.viewport.height}</div>
                  </div>
                  <div style={{ backgroundColor: '#0d1017', padding: '10px', borderRadius: '6px', border: '1px solid #21283b' }}>
                    <div style={{ color: '#8b949e', fontSize: '10px', marginBottom: '4px' }}>CRASH URL</div>
                    <div style={{ fontWeight: 600, color: '#00f0ff', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{diagnostic.latest_event.context.url}</div>
                  </div>
                </div>
              </div>

            </div>
          )}
        </div>

      </div>
    </div>
  );
}

export default App;
