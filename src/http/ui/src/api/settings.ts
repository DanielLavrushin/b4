import { apiGet, apiPost, apiFetch, apiUpload } from "./apiClient";
import { B4Config } from "@models/config";
import {
  GeoFileInfo,
  GeodatDownloadResult,
  GeodatFileType,
  GeodatRemoveResult,
  GeodatSource,
  RestartResponse,
  SystemInfo,
  UpdateResponse,
} from "@b4.settings";

// Config API
export const configApi = {
  get: () => apiGet<B4Config>("/api/config"),
  save: (config: B4Config) =>
    apiFetch<void>("/api/config", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(config),
    }),
  reset: () => apiPost<void>("/api/config/reset"),
};

// Geodat API
export const geodatApi = {
  sources: () => apiGet<GeodatSource[]>("/api/geodat/sources"),
  info: (path: string) =>
    apiGet<GeoFileInfo>(`/api/geodat/info?path=${encodeURIComponent(path)}`),
  download: (destPath: string, geositeUrl?: string, geoipUrl?: string) =>
    apiPost<GeodatDownloadResult>("/api/geodat/download", {
      geosite_url: geositeUrl ?? "",
      geoip_url: geoipUrl ?? "",
      destination_path: destPath,
    }),
  remove: (type: GeodatFileType | "both") =>
    apiPost<GeodatRemoveResult>("/api/geodat/remove", { type }),
  upload: (file: File, type: "geosite" | "geoip", destPath: string) => {
    const formData = new FormData();
    formData.append("file", file);
    formData.append("type", type);
    formData.append("destination_path", destPath);
    return apiUpload<{ success: boolean; message: string; path: string; size: number }>(
      "/api/geodat/upload",
      formData,
    );
  },
};

// System API
export const systemApi = {
  info: () => apiGet<SystemInfo>("/api/system/info"),
  restart: () => apiPost<RestartResponse>("/api/system/restart"),
  update: (version?: string) =>
    apiPost<UpdateResponse>("/api/system/update", { version }),
  version: () => apiGet<unknown>("/api/version"),
};
