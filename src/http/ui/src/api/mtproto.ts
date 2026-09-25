import { ApiError, apiDelete, apiGet, apiPost, apiUpload } from "./apiClient";
import { TelegramBridgeStatus } from "@models/mtproto";

export interface WebProxyPageStatus {
  success: boolean;
  custom: boolean;
  path: string;
  max_size: number;
  size?: number;
  modified?: string;
}

const webProxyPageDownloadUrl = "/api/mtproto/web-proxy/page?download=1";

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
  downloadWebProxyPage: async () => {
    const r = await fetch(webProxyPageDownloadUrl);
    if (!r.ok) {
      let body: unknown;
      try {
        body = await r.json();
      } catch {
        body = await r.text().catch(() => undefined);
      }
      throw new ApiError(webProxyPageDownloadUrl, r.status, r.statusText, body);
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
