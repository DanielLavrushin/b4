import { apiGet, apiUpload, apiPost, apiDelete } from "./apiClient";
import {
  Capture,
  CaptureProbeResponse,
  CaptureUploadResponse,
} from "@b4.capture";

export const captureApi = {
  list: () => apiGet<Capture[]>("/api/capture/list"),
  generate: (domain: string, protocol: string) =>
    apiPost<CaptureProbeResponse>("/api/capture/generate", {
      domain,
      protocol,
    }),
  probe: (domain: string, protocol: string) =>
    apiPost<CaptureProbeResponse>("/api/capture/probe", { domain, protocol }),
  delete: (protocol: string, domain: string) =>
    apiDelete(`/api/capture/delete?protocol=${protocol}&domain=${domain}`),
  clear: () => apiPost<{ success: boolean }>("/api/capture/clear"),
  upload: (file: File, domain: string, protocol: string) => {
    const formData = new FormData();
    formData.append("file", file);
    formData.append("domain", domain);
    formData.append("protocol", protocol);
    return apiUpload<CaptureUploadResponse>("/api/capture/upload", formData);
  },
};
