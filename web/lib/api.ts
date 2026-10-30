"use client";

import { useCallback, useEffect, useState } from "react";

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
    public fieldErrors?: Record<string, string>,
    public requestId?: string,
  ) {
    super(message);
  }
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  version?: number;
  idempotencyKey?: string;
}

export async function api<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const headers: Record<string, string> = { "X-TaskForce-Client": "web" };
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  if (opts.version !== undefined) headers["If-Match"] = `"${opts.version}"`;
  if (opts.idempotencyKey) headers["Idempotency-Key"] = opts.idempotencyKey;
  const res = await fetch(`/api/v1${path}`, {
    method: opts.method ?? "GET",
    headers,
    body: opts.body === undefined ? undefined : JSON.stringify(opts.body),
    credentials: "same-origin",
    cache: "no-store",
  });
  if (res.status === 204) return undefined as T;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    if (res.status === 401 && typeof window !== "undefined" && !window.location.pathname.startsWith("/login")) {
      window.location.replace(new URL("/login", window.location.origin));
    }
    throw new ApiError(res.status, data.code ?? "ERROR", data.message ?? "Request failed.", data.field_errors, data.request_id);
  }
  return data as T;
}

export function errorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    const fields = err.fieldErrors ? Object.values(err.fieldErrors) : [];
    return fields.length ? fields.join(" ") : err.message;
  }
  return "Something went wrong. Please try again.";
}

/**
 * Fetches `path` (skipped when null) and refetches whenever `refreshKey` changes.
 * Data from a previous fetch of the same path stays visible while refetching.
 */
export function useApi<T>(path: string | null, refreshKey = 0) {
  const [state, setState] = useState<{ path?: string; key?: string; data?: T; error?: ApiError }>({});
  const [tick, setTick] = useState(0);
  const reload = useCallback(() => setTick((t) => t + 1), []);
  const key = path ? `${path}#${refreshKey}#${tick}` : null;

  useEffect(() => {
    if (!path || !key) return;
    let cancelled = false;
    api<T>(path)
      .then((data) => !cancelled && setState({ path, key, data }))
      .catch((error) => !cancelled && setState({ path, key, error }));
    return () => {
      cancelled = true;
    };
  }, [path, key]);

  const samePath = state.path === path;
  return {
    data: samePath ? state.data : undefined,
    error: samePath ? state.error : undefined,
    loading: key !== null && state.key !== key,
    reload,
  };
}
