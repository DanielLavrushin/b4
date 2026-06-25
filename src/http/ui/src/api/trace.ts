import { apiGet, apiPost } from "./apiClient";

export interface TraceStatus {
  active: boolean;
  startedAt?: string;
  note?: string;
  lines: number;
  level: string;
  downloadReady: boolean;
  downloadName?: string;
  maxSeconds: number;
}

export const traceApi = {
  status: () => apiGet<TraceStatus>("/api/logs/trace/status"),
  start: (note?: string) =>
    apiPost<TraceStatus>("/api/logs/trace/start", { note: note ?? "" }),
  stop: () => apiPost<TraceStatus>("/api/logs/trace/stop"),
  download: async () => {
    const r = await fetch("/api/logs/trace/download");
    if (!r.ok) throw new Error(`download failed: ${r.status}`);
    const blob = await r.blob();
    const cd = r.headers.get("Content-Disposition") ?? "";
    const match = /filename="?([^"]+)"?/.exec(cd);
    const name = match ? match[1] : "b4-trace.log";
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = name;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  },
};
