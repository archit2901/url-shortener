"use client";

import {
  createContext,
  ReactNode,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";

import { api, clearToken, getToken, setToken } from "./api";
import { User } from "./types";

interface AuthContextValue {
  user: User | null;
  loading: boolean;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [loading, setLoading] = useState(true);

  // On mount, restore session from localStorage if a token is present.
  // We trust the token rather than re-validating against the backend on
  // every page load — if it's expired, the next API call will 401 and
  // we can clear it then.
  useEffect(() => {
    const token = getToken();
    if (token) {
      // We don't have a "me" endpoint, so we just mark as authenticated.
      // For a real app you'd add GET /api/auth/me to fetch current user.
      // For now, decode minimal info from the JWT payload.
      try {
        const payload = JSON.parse(atob(token.split(".")[1]));
        setUser({ id: payload.user_id, email: payload.email });
      } catch {
        clearToken();
      }
    }
    setLoading(false);
  }, []);

  const login = useCallback(async (email: string, password: string) => {
    const res = await api.login(email, password);
    if (res.token) {
      setToken(res.token);
    }
    setUser(res.user);
  }, []);

  const register = useCallback(async (email: string, password: string) => {
    // Register doesn't auto-login on our backend — we register then log in
    await api.register(email, password);
    const loginRes = await api.login(email, password);
    if (loginRes.token) {
      setToken(loginRes.token);
    }
    setUser(loginRes.user);
  }, []);

  const logout = useCallback(() => {
    clearToken();
    setUser(null);
  }, []);

  return (
    <AuthContext.Provider value={{ user, loading, login, register, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error("useAuth must be used inside <AuthProvider>");
  }
  return ctx;
}