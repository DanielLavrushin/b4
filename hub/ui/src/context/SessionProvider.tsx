import {
  createContext,
  use,
  useCallback,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";
import { useQueryClient } from "@tanstack/react-query";
import { ApiError, onUnauthorized } from "@/api/client";
import { fetchSession, login as apiLogin, logout as apiLogout } from "@/api/hub";
import type { SessionState } from "@/models/api";

interface SessionContextValue {
  loading: boolean;
  configured: boolean;
  authenticated: boolean;
  version: string;
  login: (password: string) => Promise<ApiError | null>;
  logout: () => Promise<void>;
}

const SessionContext = createContext<SessionContextValue | null>(null);

const initial: SessionState = { configured: true, authenticated: false, version: "" };

export function SessionProvider({ children }: Readonly<{ children: ReactNode }>) {
  const [state, setState] = useState<SessionState>(initial);
  const [loading, setLoading] = useState(true);
  const client = useQueryClient();

  useEffect(() => {
    let cancelled = false;
    fetchSession()
      .then((session) => {
        if (!cancelled) setState(session);
      })
      .catch(() => {
        if (!cancelled) setState(initial);
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(
    () =>
      onUnauthorized(() => {
        setState((s) => ({ ...s, authenticated: false }));
        client.clear();
      }),
    [client],
  );

  const login = useCallback(async (password: string) => {
    try {
      const session = await apiLogin(password);
      setState(session);
      return null;
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.status === 503) {
          setState((s) => ({ ...s, configured: false }));
        }
        return err;
      }
      return new ApiError(0, "network", err instanceof Error ? err.message : String(err));
    }
  }, []);

  const logout = useCallback(async () => {
    try {
      await apiLogout();
    } finally {
      setState((s) => ({ ...s, authenticated: false }));
      client.clear();
    }
  }, [client]);

  const value = useMemo<SessionContextValue>(
    () => ({
      loading,
      configured: state.configured,
      authenticated: state.authenticated,
      version: state.version,
      login,
      logout,
    }),
    [loading, state, login, logout],
  );

  return <SessionContext value={value}>{children}</SessionContext>;
}

export const useSession = (): SessionContextValue => {
  const ctx = use(SessionContext);
  if (!ctx) throw new Error("useSession outside SessionProvider");
  return ctx;
};
