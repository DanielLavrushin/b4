import { apiPost, apiGet, apiDelete } from "./apiClient";
import type {
  DetectorLists,
  DetectorOptions,
  DetectorStartResponse,
  DetectorSuite,
} from "@models/detector";

export const detectorApi = {
  start: (options: DetectorOptions) =>
    apiPost<DetectorStartResponse>("/api/detector/start", options),
  current: () => apiGet<DetectorSuite | null>("/api/detector/current"),
  status: (id: string) => apiGet<DetectorSuite>(`/api/detector/status/${id}`),
  cancel: (id: string) => apiDelete(`/api/detector/cancel/${id}`),
  history: () => apiGet<DetectorSuite[]>("/api/detector/history"),
  clearHistory: () => apiPost("/api/detector/history/clear", {}),
  deleteHistoryEntry: (id: string) =>
    apiDelete(`/api/detector/history/${encodeURIComponent(id)}`),
  lists: () => apiGet<DetectorLists>("/api/detector/lists"),
  updateLists: () => apiPost<DetectorLists>("/api/detector/lists/update", {}),
  resetLists: () => apiPost<DetectorLists>("/api/detector/lists/reset", {}),
};
