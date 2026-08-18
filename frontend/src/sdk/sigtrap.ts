/**
 * SigTrap Browser SDK
 *
 * Usage:
 *   SigTrap.init({ projectKey: '...', endpoint: 'http://localhost:8080/api/v1/trap' });
 *   SigTrap.addBreadcrumb({ category: 'ui.click', message: 'Submit button' });
 *   SigTrap.captureException(new Error('boom'));
 *
 * Auto-captures:
 *   - window.onerror
 *   - window.addEventListener('unhandledrejection', ...)
 *   - DOM click events → breadcrumbs
 *   - fetch() calls → breadcrumbs (monkey-patched)
 */

import type {
  Breadcrumb,
  StackFrame,
  TrapPayload,
  Context,
} from '../types/sigtrap';
import { RingBuffer } from './ringBuffer';

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

export interface SigTrapConfig {
  projectKey: string;
  endpoint: string;
  environment?: string;
  releaseVersion?: string;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Parse an Error.stack string into an array of StackFrames.
 *
 * Handles two common Chrome/V8 formats:
 *   at functionName (https://example.com/bundle.js:1:4892)
 *   at https://example.com/bundle.js:1:4892
 */
function parseStack(error: Error): StackFrame[] {
  const stack = error.stack ?? '';
  const frames: StackFrame[] = [];

  // Named frame:   at funcName (file:line:col)
  const namedRe = /^\s*at\s+([\w$.<>[\] ]+)\s+\((.+):(\d+):(\d+)\)\s*$/;
  // Anonymous frame: at file:line:col
  const anonRe  = /^\s*at\s+(.+):(\d+):(\d+)\s*$/;

  for (const rawLine of stack.split('\n')) {
    let m = rawLine.match(namedRe);
    if (m) {
      frames.push({
        filename: m[2],
        function: m[1],
        lineno:   parseInt(m[3], 10),
        colno:    parseInt(m[4], 10),
      });
      continue;
    }

    m = rawLine.match(anonRe);
    if (m) {
      frames.push({
        filename: m[1],
        lineno:   parseInt(m[2], 10),
        colno:    parseInt(m[3], 10),
      });
    }
  }

  return frames;
}

/** Build the browser Context object from live window/navigator state. */
function buildContext(): Context {
  return {
    browser: navigator.userAgent,
    os:      navigator.platform,
    url:     window.location.href,
    viewport: {
      width:  window.innerWidth,
      height: window.innerHeight,
    },
  };
}

/** Generate a UUID v4 using the Web Crypto API. */
function uuid(): string {
  return crypto.randomUUID();
}

// ---------------------------------------------------------------------------
// SigTrap singleton
// ---------------------------------------------------------------------------

class SigTrapSDK {
  private config: SigTrapConfig | null = null;
  private breadcrumbs: RingBuffer<Breadcrumb> = new RingBuffer<Breadcrumb>(50);
  private initialized = false;
  private originalFetch: typeof window.fetch | null = null;

  // -------------------------------------------------------------------------
  // Public API
  // -------------------------------------------------------------------------

  /**
   * Initialize the SDK and attach global error listeners.
   * Must be called once, as early as possible in your app.
   */
  init(config: SigTrapConfig): void {
    if (this.initialized) return;
    this.config = config;
    this.initialized = true;

    this.breadcrumbs.clear();
    this.attachGlobalErrorHandlers();
    this.patchFetch();
    this.attachClickTracker();
  }

  /**
   * Record an event into the breadcrumb ring buffer.
   * Automatically fills in timestamp and data if omitted.
   */
  addBreadcrumb(crumb: Partial<Breadcrumb> & { category: string; message: string }): void {
    const full: Breadcrumb = {
      timestamp: crumb.timestamp ?? Date.now(),
      category:  crumb.category,
      message:   crumb.message,
      data:      crumb.data ?? {},
    };
    this.breadcrumbs.push(full);
  }

  /**
   * Manually capture and send an exception to the backend.
   * Called automatically by the global error handlers, but can also be
   * called directly for caught exceptions.
   */
  captureException(error: Error, overrideType?: string): void {
    if (!this.config) {
      console.warn('[SigTrap] captureException called before init()');
      return;
    }

    const frames = parseStack(error);

    const payload: TrapPayload = {
      event_id:        uuid(),
      timestamp:       Date.now(),
      release_version: this.config.releaseVersion ?? 'unknown',
      environment:     this.config.environment ?? 'production',
      exception: {
        type:       overrideType ?? error.name ?? 'Error',
        value:      error.message,
        stacktrace: frames,
      },
      breadcrumbs: this.breadcrumbs.toArray(),
      context:     buildContext(),
      sdk: {
        name:    'sigtrap-browser',
        version: '1.0.0',
      },
    };

    this.send(payload);
  }

  // -------------------------------------------------------------------------
  // Private helpers
  // -------------------------------------------------------------------------

  private attachGlobalErrorHandlers(): void {
    window.onerror = (
      _message: string | Event,
      _source?: string,
      _lineno?: number,
      _colno?: number,
      error?: Error,
    ) => {
      if (error) {
        this.captureException(error);
      } else {
        // No Error object — synthesize one from the raw fields
        const synth = new Error(String(_message));
        synth.name = 'UncaughtError';
        if (_source && _lineno != null && _colno != null) {
          synth.stack = `UncaughtError: ${_message}\n    at ${_source}:${_lineno}:${_colno}`;
        }
        this.captureException(synth);
      }
      // Return false so the error still propagates to the console
      return false;
    };

    window.addEventListener('unhandledrejection', (event: PromiseRejectionEvent) => {
      const reason = event.reason;
      if (reason instanceof Error) {
        this.captureException(reason, 'UnhandledRejection');
      } else {
        const synth = new Error(String(reason));
        synth.name = 'UnhandledRejection';
        this.captureException(synth, 'UnhandledRejection');
      }
    });
  }

  /** Monkey-patch window.fetch to capture network breadcrumbs. */
  private patchFetch(): void {
    this.originalFetch = window.fetch.bind(window);
    const sdk = this;
    const original = this.originalFetch;

    window.fetch = async function patchedFetch(
      input: RequestInfo | URL,
      init?: RequestInit,
    ): Promise<Response> {
      const method = init?.method?.toUpperCase() ?? 'GET';
      const url = typeof input === 'string'
        ? input
        : input instanceof URL
          ? input.href
          : (input as Request).url;

      const startMs = Date.now();

      try {
        const response = await original(input, init);
        const latencyMs = Date.now() - startMs;

        // Extract a short path for readability
        let shortUrl = url;
        try { shortUrl = new URL(url, window.location.href).pathname; } catch { /* ignore */ }

        sdk.addBreadcrumb({
          category: 'network.fetch',
          message:  `${method} ${shortUrl}`,
          data: {
            status_code: response.status,
            latency_ms:  latencyMs,
          },
        });

        return response;
      } catch (err) {
        const latencyMs = Date.now() - startMs;
        let shortUrl = url;
        try { shortUrl = new URL(url, window.location.href).pathname; } catch { /* ignore */ }

        sdk.addBreadcrumb({
          category: 'network.fetch',
          message:  `${method} ${shortUrl}`,
          data: {
            status_code: 0,
            latency_ms:  latencyMs,
            error:       String(err),
          },
        });

        throw err;
      }
    };
  }

  /** Capture DOM click events as ui.click breadcrumbs. */
  private attachClickTracker(): void {
    document.addEventListener(
      'click',
      (event: MouseEvent) => {
        const target = event.target as HTMLElement | null;
        if (!target) return;

        // Build a useful CSS-selector-style description of the clicked element
        let desc = target.tagName.toLowerCase();
        if (target.id) desc += `#${target.id}`;
        if (target.className && typeof target.className === 'string') {
          const classes = target.className.trim().split(/\s+/).slice(0, 3).join('.');
          if (classes) desc += `.${classes}`;
        }

        this.addBreadcrumb({
          category: 'ui.click',
          message:  desc,
        });
      },
      { capture: true, passive: true },
    );
  }

  /** POST the payload to the ingestion endpoint. */
  private send(payload: TrapPayload): void {
    if (!this.config) return;

    const body = JSON.stringify(payload);

    // Use the original fetch (pre-patch) to avoid infinite breadcrumb loops
    const fetchFn = this.originalFetch ?? window.fetch.bind(window);

    fetchFn(this.config.endpoint, {
      method:    'POST',
      keepalive: true, // fires even if the page is unloading
      headers: {
        'Content-Type':           'application/json',
        'X-SigTrap-Project-Key':  this.config.projectKey,
      },
      body,
    }).catch((err: unknown) => {
      // Non-fatal: log quietly but don't re-throw
      console.warn('[SigTrap] Failed to send event:', err);
    });
  }
}

// Export a single shared instance
export const SigTrap = new SigTrapSDK();
