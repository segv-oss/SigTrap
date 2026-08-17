// SIGTRAP Browser SDK - Zero Dependency Telemetry & Ring Buffer Collector

export interface Breadcrumb {
  timestamp: number;
  category: 'ui.click' | 'network.fetch' | 'navigation' | 'console.error';
  message: string;
  data?: Record<string, unknown>;
}

export interface StackFrame {
  filename: string;
  function?: string;
  lineno: number;
  colno: number;
}

export interface TrapPayload {
  event_id: string;
  timestamp: number;
  release_version: string;
  environment: string;
  exception: {
    type: string;
    value: string;
    stacktrace: StackFrame[];
  };
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
  user?: {
    id?: string;
    email?: string;
  };
  sdk?: {
    name: string;
    version: string;
  };
}

export interface SigTrapConfig {
  projectKey: string;
  endpoint?: string;
  releaseVersion?: string;
  environment?: string;
  maxBreadcrumbs?: number;
}

class SigTrapSDK {
  private config: SigTrapConfig | null = null;
  private ringBuffer: Breadcrumb[] = [];
  private maxBreadcrumbs = 50;
  private initialized = false;

  public init(config: SigTrapConfig) {
    if (this.initialized) return;

    this.config = {
      endpoint: 'http://localhost:8080/api/v1/trap',
      releaseVersion: 'v1.4.2-ab89c2',
      environment: 'production',
      maxBreadcrumbs: 50,
      ...config,
    };

    this.maxBreadcrumbs = this.config.maxBreadcrumbs || 50;
    this.initialized = true;

    this.setupListeners();
    this.setupNetworkInterceptor();
    this.setupDOMClickCapturer();

    this.addBreadcrumb({
      category: 'navigation',
      message: `Navigated to ${window.location.href}`,
    });

    console.log('⚡ SigTrap SDK Initialized [Project Key:', this.config.projectKey, ']');
  }

  public addBreadcrumb(breadcrumb: Omit<Breadcrumb, 'timestamp'>) {
    const entry: Breadcrumb = {
      timestamp: Date.now(),
      ...breadcrumb,
    };

    this.ringBuffer.push(entry);
    if (this.ringBuffer.length > this.maxBreadcrumbs) {
      this.ringBuffer.shift(); // Maintain 50-item ring buffer
    }
  }

  public getBreadcrumbs(): Breadcrumb[] {
    return [...this.ringBuffer];
  }

  public captureException(error: Error | ErrorEvent | PromiseRejectionEvent, customType?: string) {
    if (!this.config || !this.initialized) return;

    let type = customType || 'UnhandledException';
    let value = 'Unknown error';
    let stacktrace: StackFrame[] = [];

    if (error instanceof Error) {
      type = error.name || type;
      value = error.message;
      stacktrace = this.parseStackTrace(error.stack);
    } else if (typeof ErrorEvent !== 'undefined' && error instanceof ErrorEvent) {
      type = error.error?.name || 'ErrorEvent';
      value = error.message || 'Script error';
      if (error.error?.stack) {
        stacktrace = this.parseStackTrace(error.error.stack);
      } else {
        stacktrace = [{
          filename: error.filename || window.location.href,
          function: 'global',
          lineno: error.lineno || 1,
          colno: error.colno || 1,
        }];
      }
    } else if (typeof PromiseRejectionEvent !== 'undefined' && error instanceof PromiseRejectionEvent) {
      type = 'UnhandledPromiseRejection';
      value = String(error.reason?.message || error.reason || 'Unhandled Promise Rejection');
      if (error.reason?.stack) {
        stacktrace = this.parseStackTrace(error.reason.stack);
      }
    }

    if (stacktrace.length === 0) {
      stacktrace = [{
        filename: window.location.href,
        function: 'anonymous',
        lineno: 1,
        colno: 1,
      }];
    }

    const payload: TrapPayload = {
      event_id: this.generateUUID(),
      timestamp: Date.now(),
      release_version: this.config.releaseVersion || 'v1.4.2-ab89c2',
      environment: this.config.environment || 'production',
      exception: {
        type,
        value,
        stacktrace,
      },
      breadcrumbs: this.getBreadcrumbs(),
      context: {
        browser: this.detectBrowser(),
        os: this.detectOS(),
        url: window.location.href,
        viewport: {
          width: window.innerWidth,
          height: window.innerHeight,
        },
      },
      sdk: {
        name: 'sigtrap-browser',
        version: '1.0.0',
      },
    };

    this.sendTrapPayload(payload);
  }

  private sendTrapPayload(payload: TrapPayload) {
    if (!this.config?.endpoint) return;

    fetch(this.config.endpoint, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'X-SigTrap-Project-Key': this.config.projectKey,
      },
      body: JSON.stringify(payload),
    }).catch(err => {
      console.warn('⚡ SigTrap: Ingestion transport failed', err);
    });
  }

  private parseStackTrace(stack?: string): StackFrame[] {
    if (!stack) return [];
    const lines = stack.split('\n');
    const frames: StackFrame[] = [];

    for (const line of lines) {
      const match = line.match(/at\s+(?:(.+?)\s+\()?(.+?):(\d+):(\d+)\)?/);
      if (match) {
        frames.push({
          function: match[1] || 'anonymous',
          filename: match[2],
          lineno: parseInt(match[3], 10),
          colno: parseInt(match[4], 10),
        });
      }
    }
    return frames.length > 0 ? frames : [{ filename: window.location.href, lineno: 1, colno: 1 }];
  }

  private setupListeners() {
    window.addEventListener('error', (event) => {
      this.captureException(event);
    });

    window.addEventListener('unhandledrejection', (event) => {
      this.captureException(event);
    });
  }

  private setupDOMClickCapturer() {
    document.addEventListener('click', (event) => {
      const target = event.target as HTMLElement;
      if (!target) return;

      const tag = target.tagName.toLowerCase();
      const id = target.id ? `#${target.id}` : '';
      const cls = target.className && typeof target.className === 'string' ? `.${target.className.split(' ').join('.')}` : '';
      const label = target.innerText ? ` "${target.innerText.substring(0, 20)}"` : '';

      this.addBreadcrumb({
        category: 'ui.click',
        message: `${tag}${id}${cls}${label}`,
      });
    }, { capture: true, passive: true });
  }

  private setupNetworkInterceptor() {
    const originalFetch = window.fetch;
    const self = this;

    window.fetch = async function (...args) {
      const start = Date.now();
      const url = typeof args[0] === 'string' ? args[0] : (args[0] as Request).url;
      const method = (args[1]?.method || 'GET').toUpperCase();

      // Don't log SigTrap telemetry endpoint calls to avoid infinite loops
      if (self.config?.endpoint && url.includes('/api/v1/trap')) {
        return originalFetch.apply(this, args);
      }

      try {
        const response = await originalFetch.apply(this, args);
        self.addBreadcrumb({
          category: 'network.fetch',
          message: `${method} ${url}`,
          data: {
            status_code: response.status,
            latency_ms: Date.now() - start,
          },
        });
        return response;
      } catch (err) {
        self.addBreadcrumb({
          category: 'network.fetch',
          message: `${method} ${url} [FAILED]`,
          data: {
            status_code: 0,
            latency_ms: Date.now() - start,
          },
        });
        throw err;
      }
    };
  }

  private detectBrowser(): string {
    const ua = navigator.userAgent;
    if (ua.includes('Chrome')) return 'Chrome 115.0.0.0';
    if (ua.includes('Firefox')) return 'Firefox 118.0';
    if (ua.includes('Safari')) return 'Safari 16.5';
    return 'Browser Client';
  }

  private detectOS(): string {
    const ua = navigator.userAgent;
    if (ua.includes('Linux')) return 'Linux x86_64';
    if (ua.includes('Windows')) return 'Windows 11';
    if (ua.includes('Mac')) return 'macOS Sonoma';
    return 'OS Platform';
  }

  private generateUUID(): string {
    if (typeof crypto !== 'undefined' && crypto.randomUUID) {
      return crypto.randomUUID();
    }
    return '10000000-1000-4000-8000-100000000000'.replace(/[018]/g, c =>
      (parseInt(c, 10) ^ (crypto.getRandomValues(new Uint8Array(1))[0] & (15 >> (parseInt(c, 10) / 4)))).toString(16)
    );
  }
}

export const SigTrap = new SigTrapSDK();
