import type { MetricsFrame } from "../models/metrics";
import { mergeFrame, normalizeFrame, parseFrame } from "./metricsMerge";

export type MetricsLink =
  | { state: "connecting"; since: number; startup?: boolean }
  | { state: "open"; since: number }
  | { state: "retrying"; since: number; retryAt: number; attempt: number }
  | { state: "polling"; since: number }
  | { state: "auth"; since: number };

export type MetricsLinkState = MetricsLink["state"];

export interface MetricsSnapshot {
  frame: MetricsFrame | null;
  receivedAt: number;
  offsetMs: number;
  link: MetricsLink;
  stale: boolean;
}

export interface SocketHandlers {
  open: () => void;
  message: (data: unknown) => void;
  close: () => void;
}

export interface SocketHandle {
  close: () => void;
}

export interface FetchResult {
  status: number;
  body: unknown;
}

export interface MetricsEnv {
  now: () => number;
  after: (ms: number, fn: () => void) => () => void;
  random: () => number;
  openSocket: (handlers: SocketHandlers) => SocketHandle;
  fetchJson: (url: string, signal: AbortSignal) => Promise<FetchResult>;
  isHidden: () => boolean;
  onVisible: (fn: () => void) => () => void;
  onOnline: (fn: () => void) => () => void;
  onTokenChange: (fn: () => void) => () => void;
}

export interface MetricsTiming {
  idleCloseMs: number;
  firstPaintMs: number;
  pollMs: number;
  staleMs: number;
  livenessMs: number;
  livenessCheckMs: number;
  backoffMs: readonly number[];
  jitter: number;
}

export const METRICS_TIMING: MetricsTiming = {
  idleCloseMs: 10_000,
  firstPaintMs: 5_000,
  pollMs: 5_000,
  staleMs: 3_000,
  livenessMs: 10_000,
  livenessCheckMs: 2_500,
  backoffMs: [1_000, 2_000, 4_000, 8_000, 16_000, 30_000],
  jitter: 0.2,
};

export const METRICS_URL = "/api/metrics";
export const AUTH_CHECK_URL = "/api/auth/check";

interface Subscription {
  listener: () => void;
}

export class MetricsStore {
  private readonly env: MetricsEnv;
  private readonly timing: MetricsTiming;
  private snapshot: MetricsSnapshot;
  private readonly subscriptions = new Set<Subscription>();
  private running = false;
  private generation = 0;
  private socket: SocketHandle | null = null;
  private socketLive = false;
  private socketActivity = 0;
  private attempt = 0;
  private failures = 0;
  private authChecked = false;
  private framesSinceStart = 0;
  private lastPollAt = 0;
  private retryAt = 0;
  private pendingNotify = false;
  private pollAbort: AbortController | null = null;
  private authAbort: AbortController | null = null;
  private cancelRetry: (() => void) | null = null;
  private cancelPoll: (() => void) | null = null;
  private cancelFirstPaint: (() => void) | null = null;
  private cancelIdle: (() => void) | null = null;
  private cancelLiveness: (() => void) | null = null;
  private cancelStale: (() => void) | null = null;
  private unlisten: (() => void)[] = [];

  constructor(env: MetricsEnv, timing: MetricsTiming = METRICS_TIMING) {
    this.env = env;
    this.timing = timing;
    this.snapshot = {
      frame: null,
      receivedAt: 0,
      offsetMs: 0,
      link: { state: "connecting", since: env.now() },
      stale: false,
    };
  }

  readonly getSnapshot = (): MetricsSnapshot => this.snapshot;

  readonly subscribe = (listener: () => void): (() => void) => {
    const subscription: Subscription = { listener };
    this.subscriptions.add(subscription);
    if (this.subscriptions.size === 1) this.firstSubscriber();
    return () => {
      if (!this.subscriptions.delete(subscription)) return;
      if (this.subscriptions.size === 0) this.lastSubscriberLeft();
    };
  };

  readonly retryNow = (): void => {
    if (!this.running) return;
    if (this.snapshot.link.state === "auth") {
      this.failures = 0;
      this.authChecked = false;
      this.setLink({ state: "connecting", since: this.env.now() });
    }
    if (this.socket) {
      if (this.socketLive && !this.snapshot.stale) return;
      this.closeSocket();
    }
    this.clearRetry();
    this.connect();
  };

  peekOffset(): number {
    return this.snapshot.offsetMs;
  }

  get subscriberCount(): number {
    return this.subscriptions.size;
  }

  get isRunning(): boolean {
    return this.running;
  }

  private firstSubscriber(): void {
    if (this.cancelIdle) {
      this.cancelIdle();
      this.cancelIdle = null;
    }
    if (!this.running) {
      this.start();
      return;
    }
    const state = this.snapshot.link.state;
    if (state === "retrying" || state === "auth") this.retryNow();
  }

  private lastSubscriberLeft(): void {
    if (this.cancelIdle) this.cancelIdle();
    this.cancelIdle = this.env.after(this.timing.idleCloseMs, () => {
      this.cancelIdle = null;
      if (this.subscriptions.size === 0) this.stop();
    });
  }

  private start(): void {
    this.running = true;
    this.generation++;
    this.attempt = 0;
    this.failures = 0;
    this.authChecked = false;
    this.framesSinceStart = 0;
    this.lastPollAt = 0;
    this.pendingNotify = false;
    this.unlisten = [
      this.env.onVisible(this.handleVisible),
      this.env.onOnline(this.handleOnline),
      this.env.onTokenChange(this.handleTokenChange),
    ];
    this.setLink({ state: "connecting", since: this.env.now(), startup: true }, false);
    this.connect();
    const generation = this.generation;
    this.cancelFirstPaint = this.env.after(this.timing.firstPaintMs, () => {
      this.cancelFirstPaint = null;
      if (generation !== this.generation || this.framesSinceStart > 0) return;
      if (!this.pollAbort && !this.cancelPoll) void this.poll();
    });
  }

  private stop(): void {
    this.running = false;
    this.generation++;
    this.closeSocket();
    this.clearRetry();
    this.clearPoll();
    this.authAbort?.abort();
    this.authAbort = null;
    for (const cancel of [this.cancelFirstPaint, this.cancelStale]) cancel?.();
    this.cancelFirstPaint = null;
    this.cancelStale = null;
    for (const off of this.unlisten) off();
    this.unlisten = [];
    this.snapshot = {
      ...this.snapshot,
      link: { state: "connecting", since: this.env.now(), startup: true },
      stale: this.snapshot.frame !== null,
    };
  }

  private connect(): void {
    if (!this.running || this.socket || this.snapshot.link.state === "auth") return;
    const generation = this.generation;
    let handle: SocketHandle | null = null;
    const isCurrent = () => generation === this.generation && handle !== null && this.socket === handle;
    if (this.snapshot.link.state !== "polling" && this.snapshot.link.state !== "connecting") {
      this.setLink({ state: "connecting", since: this.env.now() });
    }
    try {
      handle = this.env.openSocket({
        open: () => {
          if (isCurrent()) this.socketActivity = this.env.now();
        },
        message: (data) => {
          if (isCurrent()) this.socketMessage(data);
        },
        close: () => {
          if (!isCurrent()) return;
          this.socket = null;
          this.socketDown();
        },
      });
    } catch {
      handle = null;
    }
    if (!handle) {
      this.socketDown();
      return;
    }
    this.socket = handle;
    this.socketLive = false;
    this.socketActivity = this.env.now();
    this.startLiveness();
  }

  private closeSocket(): void {
    this.stopLiveness();
    const socket = this.socket;
    this.socket = null;
    this.socketLive = false;
    socket?.close();
  }

  private startLiveness(): void {
    this.stopLiveness();
    const check = () => {
      this.cancelLiveness = null;
      if (!this.socket) return;
      if (this.env.now() - this.socketActivity > this.timing.livenessMs) {
        this.closeSocket();
        this.socketDown();
        return;
      }
      this.cancelLiveness = this.env.after(this.timing.livenessCheckMs, check);
    };
    this.cancelLiveness = this.env.after(this.timing.livenessCheckMs, check);
  }

  private stopLiveness(): void {
    this.cancelLiveness?.();
    this.cancelLiveness = null;
  }

  private socketMessage(data: unknown): void {
    this.socketActivity = this.env.now();
    const frame = parseFrame(data);
    if (!frame) return;
    this.socketLive = true;
    if (frame.type === "hello") {
      this.attempt = 0;
      this.failures = 0;
      this.authChecked = false;
    }
    this.clearRetry();
    this.clearPoll();
    this.applyFrame(frame, "open");
  }

  private socketDown(): void {
    this.stopLiveness();
    this.socketLive = false;
    if (!this.running) return;
    this.failures++;
    if (this.snapshot.link.state === "auth") return;
    const delay = this.backoff(this.attempt);
    this.attempt++;
    const now = this.env.now();
    this.retryAt = now + delay;
    this.clearRetry();
    this.cancelRetry = this.env.after(delay, () => {
      this.cancelRetry = null;
      this.connect();
    });
    if (this.snapshot.link.state !== "polling") {
      this.setLink({ state: "retrying", since: now, retryAt: this.retryAt, attempt: this.attempt });
    }
    if (this.failures >= 2 && !this.authChecked) void this.checkAuth();
    this.ensurePolling();
  }

  private backoff(attempt: number): number {
    const steps = this.timing.backoffMs;
    const base = steps[Math.min(attempt, steps.length - 1)];
    const spread = (this.env.random() * 2 - 1) * this.timing.jitter;
    return Math.max(0, Math.round(base * (1 + spread)));
  }

  private ensurePolling(): void {
    if (!this.running || this.socketLive || this.pollAbort || this.cancelPoll) return;
    if (this.snapshot.link.state === "auth") return;
    const since = this.env.now() - this.lastPollAt;
    if (this.lastPollAt === 0 || since >= this.timing.pollMs) {
      void this.poll();
      return;
    }
    this.cancelPoll = this.env.after(this.timing.pollMs - since, () => {
      this.cancelPoll = null;
      void this.poll();
    });
  }

  private clearPoll(): void {
    this.cancelPoll?.();
    this.cancelPoll = null;
    this.pollAbort?.abort();
    this.pollAbort = null;
  }

  private clearRetry(): void {
    this.cancelRetry?.();
    this.cancelRetry = null;
  }

  private async poll(): Promise<void> {
    if (!this.running || this.socketLive || this.pollAbort) return;
    if (this.snapshot.link.state === "auth") return;
    const generation = this.generation;
    const controller = new AbortController();
    this.pollAbort = controller;
    this.lastPollAt = this.env.now();
    let frame: MetricsFrame | null = null;
    let unauthorized = false;
    try {
      const result = await this.env.fetchJson(METRICS_URL, controller.signal);
      unauthorized = result.status === 401;
      if (result.status === 200) frame = normalizeFrame(result.body);
    } catch {
      frame = null;
    }
    if (this.pollAbort === controller) this.pollAbort = null;
    if (controller.signal.aborted || generation !== this.generation || !this.running) return;
    if (unauthorized) {
      this.enterAuth();
      return;
    }
    if (this.socketLive) return;
    if (frame) {
      this.applyFrame(frame, "polling");
    } else if (this.snapshot.link.state === "polling") {
      this.setLink({
        state: "retrying",
        since: this.env.now(),
        retryAt: this.retryAt,
        attempt: this.attempt,
      });
    }
    if (!this.cancelPoll) {
      this.cancelPoll = this.env.after(this.timing.pollMs, () => {
        this.cancelPoll = null;
        void this.poll();
      });
    }
  }

  private async checkAuth(): Promise<void> {
    if (this.authAbort) return;
    this.authChecked = true;
    const generation = this.generation;
    const controller = new AbortController();
    this.authAbort = controller;
    let expired = false;
    try {
      const result = await this.env.fetchJson(AUTH_CHECK_URL, controller.signal);
      const body = result.body as { auth_required?: unknown; authenticated?: unknown } | null;
      expired =
        result.status === 401 ||
        (result.status === 200 &&
          body !== null &&
          typeof body === "object" &&
          body.auth_required === true &&
          body.authenticated !== true);
    } catch {
      expired = false;
    }
    if (this.authAbort === controller) this.authAbort = null;
    if (controller.signal.aborted || generation !== this.generation || !this.running) return;
    if (expired && !this.socketLive) this.enterAuth();
  }

  private enterAuth(): void {
    this.clearRetry();
    this.clearPoll();
    this.closeSocket();
    this.setLink({ state: "auth", since: this.env.now() });
  }

  private applyFrame(frame: MetricsFrame, link: "open" | "polling"): void {
    const now = this.env.now();
    this.framesSinceStart++;
    this.cancelFirstPaint?.();
    this.cancelFirstPaint = null;
    const current = this.snapshot.link;
    this.snapshot = {
      frame: mergeFrame(this.snapshot.frame, frame),
      receivedAt: now,
      offsetMs: frame.now - now,
      link: current.state === link ? current : { state: link, since: now },
      stale: false,
    };
    this.scheduleStale();
    this.emit();
  }

  private setLink(link: MetricsLink, notify = true): void {
    const stale = this.computeStale(link, this.snapshot.receivedAt, this.snapshot.frame !== null);
    this.snapshot = { ...this.snapshot, link, stale };
    this.scheduleStale();
    if (notify) this.emit();
  }

  private staleAfter(link: MetricsLink): number {
    return link.state === "polling" ? this.timing.pollMs + this.timing.staleMs : this.timing.staleMs;
  }

  private computeStale(link: MetricsLink, receivedAt: number, hasFrame: boolean): boolean {
    if (!hasFrame) return false;
    if (link.state !== "open" && link.state !== "polling") return true;
    return this.env.now() - receivedAt > this.staleAfter(link);
  }

  private scheduleStale(): void {
    this.cancelStale?.();
    this.cancelStale = null;
    const { frame, link, receivedAt, stale } = this.snapshot;
    if (!frame || stale || !this.running) return;
    if (link.state !== "open" && link.state !== "polling") return;
    const wait = Math.max(0, receivedAt + this.staleAfter(link) - this.env.now()) + 50;
    this.cancelStale = this.env.after(wait, () => {
      this.cancelStale = null;
      const snap = this.snapshot;
      const next = this.computeStale(snap.link, snap.receivedAt, snap.frame !== null);
      if (next === snap.stale) {
        this.scheduleStale();
        return;
      }
      this.snapshot = { ...snap, stale: next };
      this.emit();
    });
  }

  private emit(): void {
    if (this.env.isHidden()) {
      this.pendingNotify = true;
      return;
    }
    this.pendingNotify = false;
    for (const subscription of [...this.subscriptions]) subscription.listener();
  }

  private readonly handleVisible = (): void => {
    if (this.pendingNotify) this.emit();
    if (!this.socket || this.snapshot.link.state === "auth") this.retryNow();
  };

  private readonly handleOnline = (): void => {
    if (!this.socket || this.snapshot.link.state === "auth") this.retryNow();
  };

  private readonly handleTokenChange = (): void => {
    if (!this.socketLive) this.retryNow();
  };
}
