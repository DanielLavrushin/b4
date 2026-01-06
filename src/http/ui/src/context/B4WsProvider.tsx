import {
  createContext,
  useContext,
  useEffect,
  useState,
  useCallback,
  useRef,
} from "react";

const MAX_BUFFER_SIZE = 2000;
const BATCH_INTERVAL_MS = 150; // Batch updates every 150ms

interface WebSocketContextType {
  logs: string[];
  domains: string[];
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
  private maxSize: number;

  constructor(maxSize: number) {
    this.maxSize = maxSize;
  }

  push(items: string[]): void {
    this.buffer.push(...items);
    if (this.buffer.length > this.maxSize) {
      this.buffer = this.buffer.slice(-this.maxSize);
    }
  }

  getAll(): string[] {
    return [...this.buffer];
  }

  clear(): void {
    this.buffer = [];
  }

  get length(): number {
    return this.buffer.length;
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
  const [domains, setDomains] = useState<string[]>([]);
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
  const pendingLinesRef = useRef<string[]>([]);
  const batchTimeoutRef = useRef<NodeJS.Timeout | null>(null);
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
    const pending = pendingLinesRef.current;
    if (pending.length === 0) return;

    pendingLinesRef.current = [];

    // Count targeted connections for badge
    let targetedCount = 0;
    for (const line of pending) {
      if (isTargetedLine(line)) {
        targetedCount++;
      }
    }

    // Update logs if not paused
    if (!pauseLogsRef.current) {
      logsBufferRef.current.push(pending);
      setLogs(logsBufferRef.current.getAll());
    }

    // Update domains if not paused
    if (!pauseDomainsRef.current) {
      domainsBufferRef.current.push(pending);
      setDomains(domainsBufferRef.current.getAll());

      if (targetedCount > 0) {
        unseenCountRef.current += targetedCount;
        setUnseenDomainsCount(unseenCountRef.current);
      }
    }
  }, []);

  // Schedule batch processing
  const scheduleBatch = useCallback(() => {
    if (batchTimeoutRef.current === null) {
      batchTimeoutRef.current = setTimeout(() => {
        batchTimeoutRef.current = null;
        processBatch();
      }, BATCH_INTERVAL_MS);
    }
  }, [processBatch]);

  // WebSocket connection
  useEffect(() => {
    const wsUrl =
      (location.protocol === "https:" ? "wss://" : "ws://") +
      location.host +
      "/api/ws/logs";

    let ws: WebSocket | null = null;
    let reconnectTimeout: NodeJS.Timeout | null = null;
    let isCleaningUp = false;

    const connect = () => {
      if (isCleaningUp) return;

      ws = new WebSocket(wsUrl);

      ws.onopen = () => {
        console.log("WebSocket connected");
      };

      ws.onmessage = (ev) => {
        const line = String(ev.data);
        pendingLinesRef.current.push(line);
        scheduleBatch();
      };

      ws.onerror = (error) => {
        console.error("WebSocket error:", error);
      };

      ws.onclose = () => {
        if (!isCleaningUp) {
          console.log("WebSocket disconnected, reconnecting in 3s...");
          reconnectTimeout = setTimeout(connect, 3000);
        }
      };
    };

    connect();

    return () => {
      isCleaningUp = true;
      if (batchTimeoutRef.current) {
        clearTimeout(batchTimeoutRef.current);
        batchTimeoutRef.current = null;
      }
      if (reconnectTimeout) {
        clearTimeout(reconnectTimeout);
      }
      if (ws) {
        ws.close();
      }
    };
  }, [scheduleBatch]);

  const clearLogs = useCallback(() => {
    logsBufferRef.current.clear();
    setLogs([]);
  }, []);

  const clearDomains = useCallback(() => {
    domainsBufferRef.current.clear();
    setDomains([]);
    unseenCountRef.current = 0;
    setUnseenDomainsCount(0);
  }, []);

  const resetDomainsBadge = useCallback(() => {
    unseenCountRef.current = 0;
    setUnseenDomainsCount(0);
  }, []);

  return (
    <WebSocketContext.Provider
      value={{
        logs,
        domains,
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
      }}
    >
      {children}
    </WebSocketContext.Provider>
  );
};

export const useWebSocket = () => {
  const ctx = useContext(WebSocketContext);
  if (!ctx)
    throw new Error("useWebSocket must be used within WebSocketProvider");
  return ctx;
};
