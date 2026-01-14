import { QueryClient } from "@tanstack/react-query";

type ContentType = "json" | "text";

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
  error?: string;
}

export class ApiError extends Error {
  constructor(
    public url: string,
    public status: number,
    public statusText: string,
    public body?: unknown,
  ) {
    super(`${status} ${statusText}`);
    this.name = "B4ApiError";
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

export async function apiFetch<T>(
  url: string,
  options?: RequestInit & { expect?: ContentType },
): Promise<T> {
  const { expect = "json", ...fetchOptions } = options ?? {};

  const r = await fetch(url, fetchOptions);

  if (!r.ok) {
    let body: unknown;
    try {
      body = await r.json();
    } catch {
      body = await r.text().catch(() => undefined);
    }
    throw new ApiError(url, r.status, r.statusText, body);
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
    body: body !== undefined ? JSON.stringify(body) : undefined,
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
    let body: unknown;
    try {
      body = await r.json();
    } catch {
      body = await r.text().catch(() => undefined);
    }
    throw new ApiError(url, r.status, r.statusText, body);
  }

  return r.json() as Promise<T>;
}
