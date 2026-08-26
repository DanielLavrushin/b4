import { QueryClient } from "@tanstack/react-query";
import { getAuthToken } from "@context/AuthProvider";

type ContentType = "json" | "text";

// Intercept all fetch calls to inject auth token for /api/ requests
const originalFetch = globalThis.fetch.bind(globalThis);
globalThis.fetch = (input: RequestInfo | URL, init?: RequestInit) => {
  let url: string;
  if (typeof input === "string") {
    url = input;
  } else if (input instanceof URL) {
    url = input.toString();
  } else {
    url = input.url;
  }
  if (url.startsWith("/api/")) {
    const token = getAuthToken();
    if (token) {
      const headers = new Headers(init?.headers);
      if (!headers.has("Authorization")) {
        headers.set("Authorization", `Bearer ${token}`);
      }
      init = { ...init, headers };
    }
  }
  return originalFetch(input, init);
};

export const apiClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 5 * 60 * 1000,
      retry: 1,
    },
  },
});

export interface ApiResponse<T> {
  success: boolean;
  data?: T;
  error?: unknown;
}

export interface FieldError {
  path: string;
  code: string;
  message: string;
  params?: Record<string, unknown>;
}

function isFieldError(value: unknown): value is FieldError {
  if (!value || typeof value !== "object") return false;
  const f = value as Record<string, unknown>;
  if (typeof f.path !== "string") return false;
  if (typeof f.code !== "string") return false;
  if (typeof f.message !== "string") return false;
  if (f.params !== undefined && (typeof f.params !== "object" || Array.isArray(f.params))) {
    return false;
  }
  return true;
}

export class ApiError extends Error {
  public code?: string;
  public fields?: FieldError[];

  constructor(
    public url: string,
    public status: number,
    public statusText: string,
    public body?: unknown,
  ) {
    const detail = ApiError.extractDetail(body);
    super(detail ? `${status}: ${detail}` : `${status} ${statusText}`);
    this.name = "B4ApiError";
    if (body && typeof body === "object") {
      const b = body as { code?: unknown; fields?: unknown };
      if (typeof b.code === "string") this.code = b.code;
      if (Array.isArray(b.fields)) {
        this.fields = b.fields.filter((f) => isFieldError(f));
      }
    }
  }

  private static extractDetail(body: unknown): string | undefined {
    if (typeof body === "string" && body.length > 0) return body.trim();
    if (body && typeof body === "object" && "error" in body) {
      const msg = (body).error;
      if (typeof msg === "string" && msg.length > 0) return msg;
    }
    return undefined;
  }

  get isNotFound() {
    return this.status === 404;
  }
  get isUnauthorized() {
    return this.status === 401;
  }
  get isForbidden() {
    return this.status === 403;
  }
  get isServerError() {
    return this.status >= 500;
  }
}

async function readErrorBody(r: Response): Promise<unknown> {
  const raw = await r.text().catch(() => "");
  if (raw.trim().length === 0) return undefined;
  try {
    return JSON.parse(raw) as unknown;
  } catch {
    return raw;
  }
}

export async function apiFetch<T>(
  url: string,
  options?: RequestInit & { expect?: ContentType },
): Promise<T> {
  const { expect = "json", ...fetchOptions } = options ?? {};

  const r = await fetch(url, fetchOptions);

  if (!r.ok) {
    throw new ApiError(url, r.status, r.statusText, await readErrorBody(r));
  }

  if (expect === "json") {
    return r.json() as Promise<T>;
  }
  return r.text() as Promise<T>;
}

export async function apiGet<T>(url: string, expect?: ContentType): Promise<T> {
  return apiFetch<T>(url, {
    method: "GET",
    expect,
  });
}

export async function apiPost<T>(
  url: string,
  body?: unknown,
  expect?: ContentType,
): Promise<T> {
  return apiFetch<T>(url, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: body === undefined ? undefined : JSON.stringify(body),
    expect,
  });
}

export async function apiPut<T>(
  url: string,
  body: unknown,
  expect?: ContentType,
): Promise<T> {
  return apiFetch<T>(url, {
    method: "PUT",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(body),
    expect,
  });
}

export async function apiDelete(
  url: string,
  expect?: ContentType,
): Promise<void> {
  return apiFetch(url, {
    method: "DELETE",
    expect,
  });
}

export async function apiUpload<T>(
  url: string,
  formData: FormData,
): Promise<T> {
  const r = await fetch(url, {
    method: "POST",
    body: formData,
  });

  if (!r.ok) {
    throw new ApiError(url, r.status, r.statusText, await readErrorBody(r));
  }

  return r.json() as Promise<T>;
}
