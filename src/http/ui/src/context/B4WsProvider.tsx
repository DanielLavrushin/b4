import {
  createContext,
  use,
  useEffect,
  useMemo,
  useState,
  useSyncExternalStore,
} from "react";
import { wsUrl } from "@utils";
import { ParsedLog, parseSniLogLine } from "@hooks/useDomainActions";

const MAX_BUFFER_SIZE = 2000;
const BATCH_INTERVAL_MS = 150; // Batch updates every 150ms

export interface LogStream {
  logs: string[];
  logsBase: number;
}

export interface ConnectionStream {
  domains: string[];
  parsedDomains: ParsedLog[];
}

interface StreamControls {
  pauseLogs: boolean;
  showAll: boolean;
  pauseDomains: boolean;
  setShowAll: (showAll: boolean) => void;
  setPauseLogs: (paused: boolean) => void;
  setPauseDomains: (paused: boolean) => void;
  clearLogs: () => void;
  clearDomains: () => void;
  resetDomainsBadge: () => void;
}

// Simple ring buffer class for efficient fixed-size storage
class RingBuffer {
  private buffer: string[] = [];
  private readonly maxSize: number;
  private dropped = 0;

  constructor(maxSize: number) {
    this.maxSize = maxSize;
  }

  push(items: string[]): void {
    this.buffer.push(...items);
    if (this.buffer.length > this.maxSize) {
      this.dropped += this.buffer.length - this.maxSize;
      this.buffer = this.buffer.slice(-this.maxSize);
    }
  }

  get base(): number {
    return this.dropped;
  }

  getAll(): string[] {
    return [...this.buffer];
  }

  clear(): void {
    this.buffer = [];
    this.dropped = 0;
  }

  get length(): number {
    return this.buffer.length;
  }
}

// Parsed ring buffer for connection lines - parsed once on ingestion so the
// raw view doesn't reparse 1000 lines on every WS batch.
class ParsedRingBuffer {
  private buffer: ParsedLog[] = [];
  private readonly maxSize: number;

  constructor(maxSize: number) {
    this.maxSize = maxSize;
  }

  push(rawLines: string[]): void {
    for (const line of rawLines) {
      const p = parseSniLogLine(line);
      if (p) this.buffer.push(p);
    }
    if (this.buffer.length > this.maxSize) {
      this.buffer = this.buffer.slice(-this.maxSize);
    }
  }

  getAll(): ParsedLog[] {
    return [...this.buffer];
  }

  clear(): void {
    this.buffer = [];
  }
}

// Check if a line represents a targeted connection
function isTargetedLine(line: string): boolean {
  const tokens = line.trim().split(",");
  if (tokens.length < 7) return false;
  const [, , hostSet, , , ipSet] = tokens;
  return !!(hostSet || ipSet);
}

class Snapshot<T> {
  private value: T;
  private stale = false;
  private readonly build: () => T;
  private readonly listeners = new Set<() => void>();

  constructor(build: () => T) {
    this.build = build;
    this.value = build();
  }

  readonly subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  readonly get = (): T => {
    if (this.stale) {
      this.stale = false;
      this.value = this.build();
    }
    return this.value;
  };

  invalidate(): void {
    this.stale = true;
  }

  notify(): void {
    if (!this.stale) return;
    for (const listener of this.listeners) listener();
  }
}

class StreamStore {
  pauseLogs = false;
  pauseDomains = false;
  private pendingLogLines: string[] = [];
  private pendingConnLines: string[] = [];
  private unseenCount = 0;
  private readonly logsBuffer = new RingBuffer(MAX_BUFFER_SIZE);
  private readonly domainsBuffer = new RingBuffer(MAX_BUFFER_SIZE);
  private readonly parsedDomainsBuffer = new ParsedRingBuffer(MAX_BUFFER_SIZE);

  readonly logs = new Snapshot<LogStream>(() => ({
    logs: this.logsBuffer.getAll(),
    logsBase: this.logsBuffer.base,
  }));

  readonly connections = new Snapshot<ConnectionStream>(() => ({
    domains: this.domainsBuffer.getAll(),
    parsedDomains: this.parsedDomainsBuffer.getAll(),
  }));

  readonly unseen = new Snapshot<number>(() => this.unseenCount);

  readonly queueLogLine = (line: string) => {
    this.pendingLogLines.push(line);
  };

  readonly queueConnLine = (line: string) => {
    this.pendingConnLines.push(line);
  };

  processBatch(): void {
    const pendingLogs = this.pendingLogLines;
    const pendingConns = this.pendingConnLines;
    if (pendingLogs.length === 0 && pendingConns.length === 0) return;

    this.pendingLogLines = [];
    this.pendingConnLines = [];

    // Diagnostic logs feed only the /logs page.
    if (pendingLogs.length > 0 && !this.pauseLogs) {
      this.logsBuffer.push(pendingLogs);
      this.logs.invalidate();
    }

    if (pendingConns.length > 0 && !this.pauseDomains) {
      this.domainsBuffer.push(pendingConns);
      this.parsedDomainsBuffer.push(pendingConns);
      this.connections.invalidate();

      let targetedCount = 0;
      for (const line of pendingConns) {
        if (isTargetedLine(line)) targetedCount++;
      }
      if (targetedCount > 0) {
        this.unseenCount += targetedCount;
        this.unseen.invalidate();
      }
    }

    this.publish();
  }

  readonly publish = () => {
    if (!document.hidden) this.logs.notify();
    this.connections.notify();
    this.unseen.notify();
  };

  readonly clearLogs = () => {
    this.logsBuffer.clear();
    this.logs.invalidate();
    this.publish();
  };

  readonly clearDomains = () => {
    this.domainsBuffer.clear();
    this.parsedDomainsBuffer.clear();
    this.unseenCount = 0;
    this.connections.invalidate();
    this.unseen.invalidate();
    this.publish();
  };

  readonly resetDomainsBadge = () => {
    this.unseenCount = 0;
    this.unseen.invalidate();
    this.publish();
  };
}

const StreamStoreContext = createContext<StreamStore | null>(null);
const StreamControlsContext = createContext<StreamControls | null>(null);

export const WebSocketProvider = ({
  children,
}: {
  children: React.ReactNode;
}) => {
  const [store] = useState(() => new StreamStore());
  const [pauseLogs, setPauseLogs] = useState(false);
  const [pauseDomains, setPauseDomains] = useState(false);
  const [showAll, setShowAll] = useState(() => {
    return localStorage.getItem("b4_connections_showall") === "true";
  });

  useEffect(() => {
    localStorage.setItem("b4_connections_showall", String(showAll));
  }, [showAll]);

  useEffect(() => {
    store.pauseLogs = pauseLogs;
  }, [store, pauseLogs]);

  useEffect(() => {
    store.pauseDomains = pauseDomains;
  }, [store, pauseDomains]);

  // WebSocket connections — diagnostic logs and connection events are now
  // separate streams. The logs stream is level-gated; the connections stream
  // is always-on (cheap fan-out, no listeners = no work).
  useEffect(() => {
    let isCleaningUp = false;
    let batchTimeout: ReturnType<typeof setTimeout> | null = null;

    const scheduleBatch = () => {
      batchTimeout ??= setTimeout(() => {
        batchTimeout = null;
        store.processBatch();
      }, BATCH_INTERVAL_MS);
    };

    const openStream = (
      path: string,
      sink: (line: string) => void,
      label: string,
    ): { close: () => void } => {
      let ws: WebSocket | null = null;
      let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;

      const connect = () => {
        if (isCleaningUp) return;
        ws = new WebSocket(wsUrl(path));
        ws.onopen = () => console.log(`${label} WebSocket connected`);
        ws.onmessage = (ev) => {
          sink(String(ev.data));
          scheduleBatch();
        };
        ws.onerror = (error) =>
          console.error(`${label} WebSocket error:`, error);
        ws.onclose = () => {
          if (!isCleaningUp) {
            console.log(`${label} WebSocket disconnected, reconnecting in 3s...`);
            reconnectTimeout = setTimeout(connect, 3000);
          }
        };
      };

      connect();

      return {
        close: () => {
          if (reconnectTimeout) clearTimeout(reconnectTimeout);
          if (ws) ws.close();
        },
      };
    };

    const logsStream = openStream("/api/ws/logs", store.queueLogLine, "Logs");
    const connStream = openStream(
      "/api/ws/connections",
      store.queueConnLine,
      "Connections",
    );
    document.addEventListener("visibilitychange", store.publish);

    return () => {
      isCleaningUp = true;
      if (batchTimeout) clearTimeout(batchTimeout);
      document.removeEventListener("visibilitychange", store.publish);
      logsStream.close();
      connStream.close();
    };
  }, [store]);

  const controls = useMemo(
    () => ({
      pauseLogs,
      showAll,
      pauseDomains,
      setShowAll,
      setPauseLogs,
      setPauseDomains,
      clearLogs: store.clearLogs,
      clearDomains: store.clearDomains,
      resetDomainsBadge: store.resetDomainsBadge,
    }),
    [store, pauseLogs, showAll, pauseDomains],
  );

  return (
    <StreamStoreContext value={store}>
      <StreamControlsContext value={controls}>{children}</StreamControlsContext>
    </StreamStoreContext>
  );
};

const useStreamStore = () => {
  const store = use(StreamStoreContext);
  if (!store)
    throw new Error("useStreamStore must be used within WebSocketProvider");
  return store;
};

export const useLogStream = (): LogStream => {
  const { logs } = useStreamStore();
  return useSyncExternalStore(logs.subscribe, logs.get);
};

export const useConnectionStream = (): ConnectionStream => {
  const { connections } = useStreamStore();
  return useSyncExternalStore(connections.subscribe, connections.get);
};

export const useUnseenDomainsCount = (): number => {
  const { unseen } = useStreamStore();
  return useSyncExternalStore(unseen.subscribe, unseen.get);
};

export const useStreamControls = () => {
  const ctx = use(StreamControlsContext);
  if (!ctx)
    throw new Error("useStreamControls must be used within WebSocketProvider");
  return ctx;
};
