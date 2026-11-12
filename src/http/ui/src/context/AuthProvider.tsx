import {
  createContext,
  use,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from "react";
import { apiPost } from "@api/apiClient";

const TOKEN_KEY = "b4_auth_token";

interface AuthContextType {
  token: string | null;
  isAuthenticated: boolean;
  isLoading: boolean;
  authRequired: boolean;
  hasSession: boolean;
  login: (username: string, password: string) => Promise<string | null>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextType | null>(null);

export function AuthProvider({
  children,
}: Readonly<{ children: React.ReactNode }>) {
  const [token, setToken] = useState<string | null>(() =>
    localStorage.getItem(TOKEN_KEY),
  );
  const [isLoading, setIsLoading] = useState(true);
  const [authRequired, setAuthRequired] = useState(false);
  const [isAuthenticated, setIsAuthenticated] = useState(false);

  // Check auth status on mount
  useEffect(() => {
    const checkAuth = async () => {
      try {
        const headers: Record<string, string> = {};
        const savedToken = localStorage.getItem(TOKEN_KEY);
        if (savedToken) {
          headers["Authorization"] = `Bearer ${savedToken}`;
        }

        const r = await fetch("/api/auth/check", { headers });
        const data = (await r.json()) as {
          auth_required?: boolean;
          authenticated?: boolean;
        };

        if (data.auth_required) {
          setAuthRequired(true);
          setIsAuthenticated(data.authenticated === true);
          if (!data.authenticated) {
            localStorage.removeItem(TOKEN_KEY);
            setToken(null);
          }
        } else {
          setAuthRequired(false);
          setIsAuthenticated(true);
        }
      } catch {
        // If check fails (server down etc.), fail closed
        setAuthRequired(true);
        setIsAuthenticated(false);
        localStorage.removeItem(TOKEN_KEY);
        setToken(null);
      } finally {
        setIsLoading(false);
      }
    };

    void checkAuth();
  }, []);

  const login = useCallback(
    async (username: string, password: string): Promise<string | null> => {
      try {
        const data = await apiPost<{ token: string }>("/api/auth/login", {
          username,
          password,
        });
        localStorage.setItem(TOKEN_KEY, data.token);
        setToken(data.token);
        setIsAuthenticated(true);
        return null;
      } catch {
        return "Invalid username or password";
      }
    },
    [],
  );

  const logout = useCallback(() => {
    const savedToken = localStorage.getItem(TOKEN_KEY);
    if (savedToken) {
      fetch("/api/auth/logout", {
        method: "POST",
        headers: { Authorization: `Bearer ${savedToken}` },
      }).catch(() => {});
    }
    localStorage.removeItem(TOKEN_KEY);
    setToken(null);
    setIsAuthenticated(false);
  }, []);

  const hasSession = !isLoading && (!authRequired || isAuthenticated);

  const value = useMemo(
    () => ({
      token,
      isAuthenticated,
      isLoading,
      authRequired,
      hasSession,
      login,
      logout,
    }),
    [token, isAuthenticated, isLoading, authRequired, hasSession, login, logout],
  );

  return <AuthContext value={value}>{children}</AuthContext>;
}

export function useAuth(): AuthContextType {
  const context = use(AuthContext);
  if (!context) {
    throw new Error("useAuth must be used within AuthProvider");
  }
  return context;
}

export function getAuthToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function watchAuthToken(onChange: () => void): () => void {
  let last = getAuthToken();
  const handler = () => {
    const current = getAuthToken();
    if (current === last) return;
    last = current;
    onChange();
  };
  window.addEventListener("storage", handler);
  return () => window.removeEventListener("storage", handler);
}

async function askSessionRejected(): Promise<boolean> {
  try {
    const headers: Record<string, string> = {};
    const token = getAuthToken();
    if (token) headers["Authorization"] = `Bearer ${token}`;
    const r = await fetch("/api/auth/check", { headers, cache: "no-store" });
    if (!r.ok) return false;
    const data = (await r.json()) as {
      auth_required?: boolean;
      authenticated?: boolean;
    };
    return data.auth_required === true && data.authenticated !== true;
  } catch {
    return false;
  }
}

let sessionCheck: Promise<boolean> | null = null;

export function sessionRejected(): Promise<boolean> {
  sessionCheck ??= askSessionRejected().finally(() => {
    sessionCheck = null;
  });
  return sessionCheck;
}
