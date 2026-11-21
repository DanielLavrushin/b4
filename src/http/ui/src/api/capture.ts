import { ApiError, apiGet, apiUpload, apiPost, apiDelete } from "./apiClient";
import { readErrorBody } from "./errorBody";
import {
  Capture,
  CaptureGenerateResponse,
  CaptureUploadResponse,
  PayloadInspection,
} from "@b4.capture";

async function fetchBytes(url: string): Promise<Uint8Array<ArrayBuffer>> {
  const r = await fetch(url);
  if (!r.ok) {
    throw new ApiError(url, r.status, r.statusText, await readErrorBody(r));
  }
  return new Uint8Array(await r.arrayBuffer());
}

export const payloadFileName = (filepath: string) =>
  filepath.split(/[\\/]/).pop() || filepath;

export const captureApi = {
  list: () => apiGet<Capture[]>("/api/capture/list"),
  bytes: (capture: Capture) =>
    fetchBytes(
      `/api/capture/download?${new URLSearchParams({ file: payloadFileName(capture.filepath) }).toString()}`,
    ),
  generate: (domain: string, protocol: string) =>
    apiPost<CaptureGenerateResponse>("/api/capture/generate", {
      domain,
      protocol,
    }),
  delete: (protocol: string, domain: string) =>
    apiDelete(
      `/api/capture/delete?${new URLSearchParams({ protocol, domain }).toString()}`,
    ),
  clear: () => apiPost<{ success: boolean }>("/api/capture/clear"),
  inspect: (file: File) => {
    const formData = new FormData();
    formData.append("file", file);
    return apiUpload<PayloadInspection>("/api/capture/inspect", formData);
  },
  upload: (file: File, domain: string, protocol: string, replace = false) => {
    const formData = new FormData();
    formData.append("file", file);
    formData.append("domain", domain);
    formData.append("protocol", protocol);
    if (replace) formData.append("replace", "1");
    return apiUpload<CaptureUploadResponse>("/api/capture/upload", formData);
  },
};
