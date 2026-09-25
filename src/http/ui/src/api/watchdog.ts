import { apiGet, apiPost, apiDelete, apiPut } from "./apiClient";
import { WatchdogState, WatchdogSetActionResult } from "@models/watchdog";

export const watchdogApi = {
  status: () => apiGet<WatchdogState>("/api/watchdog/status"),
  forceCheck: (domain: string) =>
    apiPost("/api/watchdog/check", { domain }),
  addDomain: (domain: string) =>
    apiPost("/api/watchdog/domains", { domain }),
  removeDomain: (domain: string) =>
    apiDelete(`/api/watchdog/domains/${encodeURIComponent(domain)}`),
  enable: () => apiPost("/api/watchdog/enable", {}),
  disable: () => apiPost("/api/watchdog/disable", {}),
  setEnabled: (setId: string, enabled: boolean) =>
    apiPut<WatchdogSetActionResult>(
      `/api/watchdog/sets/${encodeURIComponent(setId)}`,
      { enabled },
    ),
  checkSet: (setId: string) =>
    apiPost<WatchdogSetActionResult>(
      `/api/watchdog/sets/${encodeURIComponent(setId)}/check`,
      {},
    ),
  moveDomain: (domain: string, setId: string) =>
    apiPost(`/api/watchdog/domains/${encodeURIComponent(domain)}/move`, {
      set_id: setId,
    }),
};
