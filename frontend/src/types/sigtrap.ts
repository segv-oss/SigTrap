export interface Viewport {
  width: number;
  height: number;
}

export interface Context {
    browser: string;
    os: string;
    url: string;
    viewport: Viewport;
}

export interface StackFrame {
    filename: string;
    function: string;
    lineno: number;
    colno: number;
}

export interface Breadcrumb {
    timestamp: number;
    category: string;
    message: string;
    data: Record<string, unknown>;
}

export interface Exception {
    type: string;
    value: string;
    stacktrace: StackFrame[];
}

export interface TrapPayload {
    event_id: string;
    timestamp: number;
    release_version: string;
    environment: string;
    exception: Exception;
    breadcrumbs: Breadcrumb[];
    context: Context;
}

export interface TrapIssue {
    issue_id: string;
    type: string;
    value: string;
    culprit_file: string;
    event_count: number;
    users_affected: number;
    last_seen: number;
    status: 'unresolved' | 'resolved' | 'ignored';
}

export interface IssuesResponse {
    issues: TrapIssue[];
}

export interface AI_Analysis {
    status: string;
    root_cause_summary: string;
    suggested_patch: string;
    confidence_score: number;
}

export interface Unminified_StackFrame {
    filename: string;
    function: string;
    lineno: number;
    colno: number;
    code_context?: string[];
}

export interface Latest_Event {
    unminified_stacktrace: Unminified_StackFrame[];
}

export interface Diagnostic_Response {
    issue_id: string;
    ai_analysis: AI_Analysis;
    latest_event: Latest_Event;
}