import { apiDelete, apiGet, apiUpload } from "./apiClient";

export interface WebProxyPageStatus {
  success: boolean;
  custom: boolean;
  path: string;
  max_size: number;
  size?: number;
  modified?: string;
}

export const webProxyPageDownloadUrl = "/api/mtproto/web-proxy/page?download=1";

export const mtprotoApi = {
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
};
