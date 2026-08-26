import {
  createContext,
  use,
  useEffect,
  useMemo,
  useState,
  useCallback,
  useRef,
} from "react";
import { wsUrl } from "@utils";
import { ParsedLog, parseSniLogLine } from "@hooks/useDomainActions";

const MAX_BUFFER_SIZE = 2000;
const BATCH_INTERVAL_MS = 150; // Batch updates every 150ms

interface WebSocketContextType {
  logs: string[];
  logsBase: number;
  domains: string[];
  parsedDomains: ParsedLog[];
  pauseLogs: boolean;
  showAll: boolean;
  pauseDomains: boolean;
  unseenDomainsCount: number;
  setShowAll: (showAll: boolean) => void;
  setPauseLogs: (paused: boolean) => void;
  setPauseDomains: (paused: boolean) => void;
  clearLogs: () => void;
  clearDomains: () => void;
  resetDomainsBadge: () => void;
}

const WebSocketContext = createContext<WebSocketContextType | null>(null);

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

export const WebSocketProvider = ({
  children,
}: {
  children: React.ReactNode;
}) => {
  const [logs, setLogs] = useState<string[]>([]);
  const [logsBase, setLogsBase] = useState(0);
  const [domains, setDomains] = useState<string[]>([]);
  const [parsedDomains, setParsedDomains] = useState<ParsedLog[]>([]);
  const [pauseLogs, setPauseLogs] = useState(false);
  const [pauseDomains, setPauseDomains] = useState(false);
  const [showAll, setShowAll] = useState(() => {
    return localStorage.getItem("b4_connections_showall") === "true";
  });

  useEffect(() => {
    localStorage.setItem("b4_connections_showall", String(showAll));
  }, [showAll]);

  const [unseenDomainsCount, setUnseenDomainsCount] = useState(0);

  // Use refs to avoid stale closures and unnecessary re-renders
  const pauseLogsRef = useRef(pauseLogs);
  const pauseDomainsRef = useRef(pauseDomains);
  const logsBufferRef = useRef(new RingBuffer(MAX_BUFFER_SIZE));
  const domainsBufferRef = useRef(new RingBuffer(MAX_BUFFER_SIZE));
  const parsedDomainsBufferRef = useRef(new ParsedRingBuffer(MAX_BUFFER_SIZE));
  const pendingLogLinesRef = useRef<string[]>([]);
  const pendingConnLinesRef = useRef<string[]>([]);
  const batchTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const unseenCountRef = useRef(0);

  // Keep refs in sync
  useEffect(() => {
    pauseLogsRef.current = pauseLogs;
  }, [pauseLogs]);

  useEffect(() => {
    pauseDomainsRef.current = pauseDomains;
  }, [pauseDomains]);

  // Batch processing function
  const processBatch = useCallback(() => {
    const pendingLogs = pendingLogLinesRef.current;
    const pendingConns = pendingConnLinesRef.current;
    if (pendingLogs.length === 0 && pendingConns.length === 0) return;

    pendingLogLinesRef.current = [];
    pendingConnLinesRef.current = [];

    // Diagnostic logs feed only the /logs page.
    if (pendingLogs.length > 0 && !pauseLogsRef.current) {
      logsBufferRef.current.push(pendingLogs);
      setLogs(logsBufferRef.current.getAll());
      setLogsBase(logsBufferRef.current.base);
    }

    if (pendingConns.length > 0 && !pauseDomainsRef.current) {
      domainsBufferRef.current.push(pendingConns);
      parsedDomainsBufferRef.current.push(pendingConns);
      setDomains(domainsBufferRef.current.getAll());
      setParsedDomains(parsedDomainsBufferRef.current.getAll());

      let targetedCount = 0;
      for (const line of pendingConns) {
        if (isTargetedLine(line)) targetedCount++;
      }
      if (targetedCount > 0) {
        unseenCountRef.current += targetedCount;
        setUnseenDomainsCount(unseenCountRef.current);
      }
    }
  }, []);

  // Schedule batch processing
  const scheduleBatch = useCallback(() => {
    batchTimeoutRef.current ??= setTimeout(() => {
      batchTimeoutRef.current = null;
      processBatch();
    }, BATCH_INTERVAL_MS);
  }, [processBatch]);

  // WebSocket connections — diagnostic logs and connection events are now
  // separate streams. The logs stream is level-gated; the connections stream
  // is always-on (cheap fan-out, no listeners = no work).
  useEffect(() => {
    let isCleaningUp = false;

    const openStream = (
      path: string,
      sink: { current: string[] },
      label: string,
    ): { close: () => void } => {
      let ws: WebSocket | null = null;
      let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;

      const connect = () => {
        if (isCleaningUp) return;
        ws = new WebSocket(wsUrl(path));
        ws.onopen = () => console.log(`${label} WebSocket connected`);
        ws.onmessage = (ev) => {
          sink.current.push(String(ev.data));
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

    const logsStream = openStream("/api/ws/logs", pendingLogLinesRef, "Logs");
    const connStream = openStream(
      "/api/ws/connections",
      pendingConnLinesRef,
      "Connections",
    );

    return () => {
      isCleaningUp = true;
      if (batchTimeoutRef.current) {
        clearTimeout(batchTimeoutRef.current);
        batchTimeoutRef.current = null;
      }
      logsStream.close();
      connStream.close();
    };
  }, [scheduleBatch]);

  const clearLogs = useCallback(() => {
    logsBufferRef.current.clear();
    setLogs([]);
    setLogsBase(0);
  }, []);

  const clearDomains = useCallback(() => {
    domainsBufferRef.current.clear();
    parsedDomainsBufferRef.current.clear();
    setDomains([]);
    setParsedDomains([]);
    unseenCountRef.current = 0;
    setUnseenDomainsCount(0);
  }, []);

  const resetDomainsBadge = useCallback(() => {
    unseenCountRef.current = 0;
    setUnseenDomainsCount(0);
  }, []);

  const contextValue = useMemo(
    () => ({
      logs,
      logsBase,
      domains,
      parsedDomains,
      pauseLogs,
      pauseDomains,
      unseenDomainsCount,
      showAll,
      setShowAll,
      setPauseLogs,
      setPauseDomains,
      clearLogs,
      clearDomains,
      resetDomainsBadge,
    }),
    [
      logs,
      logsBase,
      domains,
      parsedDomains,
      pauseLogs,
      pauseDomains,
      unseenDomainsCount,
      showAll,
      clearLogs,
      clearDomains,
      resetDomainsBadge,
    ],
  );

  return <WebSocketContext value={contextValue}>{children}</WebSocketContext>;
};

export const useWebSocket = () => {
  const ctx = use(WebSocketContext);
  if (!ctx)
    throw new Error("useWebSocket must be used within WebSocketProvider");
  return ctx;
};
