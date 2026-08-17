import { apiGet, apiFetch } from "./apiClient";

export interface AIStatus {
  enabled: boolean;
  provider: string;
  model: string;
  endpoint: string;
  api_key_ref: string;
  has_key: boolean;
  ready: boolean;
  not_ready_reason?: string;
  available_providers: string[];
}

export interface AISecretsList {
  refs: string[];
}

export interface AIModel {
  id: string;
  display_name?: string;
  created?: number;
}

export interface AIModelList {
  provider: string;
  models: AIModel[];
}

export const mcpApi = {
  generateToken: () =>
    apiFetch<{ success: boolean; token: string }>("/api/mcp/generate-token", {
      method: "POST",
    }),
};

export const aiApi = {
  status: () => apiGet<AIStatus>("/api/ai/status"),
  listSecrets: () => apiGet<AISecretsList>("/api/ai/secrets"),
  setSecret: (ref: string, key: string) =>
    apiFetch<{ success: boolean; ref: string }>("/api/ai/secrets", {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ ref, key }),
    }),
  deleteSecret: (ref: string) =>
    apiFetch<{ success: boolean }>(
      `/api/ai/secrets?ref=${encodeURIComponent(ref)}`,
      { method: "DELETE" },
    ),
  listModels: (provider?: string, endpoint?: string) => {
    const params = new URLSearchParams();
    if (provider) params.set("provider", provider);
    if (endpoint) params.set("endpoint", endpoint);
    const qs = params.toString();
    return apiGet<AIModelList>(`/api/ai/models${qs ? `?${qs}` : ""}`);
  },
};
