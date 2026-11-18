import { ApiError, apiDelete, apiGet, apiPost, apiUpload } from "./apiClient";
import { readErrorBody } from "./errorBody";
import { readSSE } from "./sse";
import { TelegramBridgeStatus } from "@models/mtproto";

export interface WebProxyPageStatus {
  success: boolean;
  custom: boolean;
  path: string;
  max_size: number;
  size?: number;
  modified?: string;
}

export interface TransportProbeResult {
  transport: string;
  ok: boolean;
  stage?: string;
  latency_ms?: number;
  hold_ms?: number;
  error?: string;
}

export interface TransportProbeHandlers {
  onPlan: (routes: string[]) => void;
  onStart: (index: number) => void;
  onResult: (index: number, result: TransportProbeResult) => void;
}

const transportProbeUrl = "/api/mtproto/test-ws";
const webProxyPageDownloadUrl = "/api/mtproto/web-proxy/page?download=1";

const probeErrorMessage = (body: unknown, r: Response) => {
  if (body && typeof body === "object" && "error" in body) {
    if (typeof body.error === "string" && body.error) return body.error;
  }
  if (typeof body === "string" && body.trim()) return body.trim();
  return `${r.status} ${r.statusText}`;
};

const dispatchProbeEvent = (
  event: string,
  data: string,
  handlers: TransportProbeHandlers,
) => {
  switch (event) {
    case "plan":
      handlers.onPlan((JSON.parse(data) as { routes: string[] }).routes);
      return;
    case "start":
      handlers.onStart((JSON.parse(data) as { index: number }).index);
      return;
    case "result": {
      const { index, ...result } = JSON.parse(data) as TransportProbeResult & {
        index: number;
      };
      handlers.onResult(index, result);
    }
  }
};

export const mtprotoApi = {
  telegramBridge: (check = false) =>
    apiGet<TelegramBridgeStatus>(
      check ? "/api/mtproto/bridge?check=1" : "/api/mtproto/bridge",
    ),
  refreshTelegramBridge: () =>
    apiPost<TelegramBridgeStatus>("/api/mtproto/bridge/refresh"),
  webProxyPage: () => apiGet<WebProxyPageStatus>("/api/mtproto/web-proxy/page"),
  uploadWebProxyPage: (file: File) => {
    const formData = new FormData();
    formData.append("file", file);
    return apiUpload<{ success: boolean; size: number }>(
      "/api/mtproto/web-proxy/page",
      formData,
    );
  },
  removeWebProxyPage: () => apiDelete("/api/mtproto/web-proxy/page"),
  probeTransports: async (
    body: Record<string, unknown>,
    handlers: TransportProbeHandlers,
    signal?: AbortSignal,
  ): Promise<boolean> => {
    const r = await fetch(transportProbeUrl, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        Accept: "text/event-stream",
      },
      body: JSON.stringify(body),
      signal,
    });
    if (!r.ok) {
      throw new Error(probeErrorMessage(await readErrorBody(r), r));
    }
    if (!r.body) return false;
    return readSSE(r.body, ({ event, data }) => {
      if (event === "done") return true;
      dispatchProbeEvent(event, data, handlers);
      return false;
    });
  },
  downloadWebProxyPage: async () => {
    const r = await fetch(webProxyPageDownloadUrl);
    if (!r.ok) {
      throw new ApiError(
        webProxyPageDownloadUrl,
        r.status,
        r.statusText,
        await readErrorBody(r),
      );
    }
    const blob = await r.blob();
    const match = /filename="?([^"]+)"?/.exec(
      r.headers.get("Content-Disposition") ?? "",
    );
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = match ? match[1] : "webproxy_page.html";
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(url);
  },
};
