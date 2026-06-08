import {
  AuthResponse,
  ClickStats,
  ShortenResponse,
  URLListResponse,
} from "./types";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
const TOKEN_KEY = "auth_token";

// --- Token storage helpers ---

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string): void {
  if (typeof window === "undefined") return;
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken(): void {
  if (typeof window === "undefined") return;
  localStorage.removeItem(TOKEN_KEY);
}

// --- Error handling ---

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
    this.name = "ApiError";
  }
}

// --- Core request helper ---

interface RequestOptions {
  method?: "GET" | "POST" | "PUT" | "DELETE";
  body?: unknown;
  auth?: boolean;
}

async function request<T>(
  path: string,
  options: RequestOptions = {}
): Promise<T> {
  const { method = "GET", body, auth = false } = options;

  const headers: Record<string, string> = {
    "Content-Type": "application/json",
  };

  if (auth) {
    const token = getToken();
    if (!token) {
      throw new ApiError(401, "not authenticated");
    }
    headers["Authorization"] = `Bearer ${token}`;
  }

  const res = await fetch(`${API_URL}${path}`, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });

  // Empty bodies (redirects, 204s) shouldn't be parsed
  if (res.status === 204 || res.status === 302) {
    return undefined as T;
  }

  const data = await res.json().catch(() => ({}));

  if (!res.ok) {
    const message =
      typeof data === "object" && data && "error" in data
        ? String(data.error)
        : `request failed with status ${res.status}`;
    throw new ApiError(res.status, message);
  }

  return data as T;
}

// --- Public API ---

export const api = {
  register: (email: string, password: string) =>
    request<AuthResponse>("/api/auth/register", {
      method: "POST",
      body: { email, password },
    }),

  login: (email: string, password: string) =>
    request<AuthResponse>("/api/auth/login", {
      method: "POST",
      body: { email, password },
    }),

  shorten: (url: string) =>
    request<ShortenResponse>("/api/shorten", {
      method: "POST",
      body: { url },
      auth: true,
    }),

  listURLs: (limit = 20, offset = 0) =>
    request<URLListResponse>(`/api/urls?limit=${limit}&offset=${offset}`, {
      auth: true,
    }),

  getStats: (code: string) =>
    request<ClickStats>(`/api/urls/${code}/stats`, { auth: true }),
};