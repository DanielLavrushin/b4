import { useState, useCallback } from "react";

interface SystemInfo {
  service_manager: string;
  os: string;
  arch: string;
  can_restart: boolean;
}

interface RestartResponse {
  success: boolean;
  message: string;
  service_manager: string;
  restart_command?: string;
}

export const useSystemRestart = () => {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const getSystemInfo = useCallback(async (): Promise<SystemInfo | null> => {
    try {
      const response = await fetch("/api/system/info");
      if (!response.ok) {
        throw new Error("Failed to get system info");
      }
      return (await response.json()) as SystemInfo;
    } catch (err) {
      console.error("Error getting system info:", err);
      return null;
    }
  }, []);

  const restart = useCallback(async (): Promise<RestartResponse | null> => {
    setLoading(true);
    setError(null);

    try {
      const response = await fetch("/api/system/restart", {
        method: "POST",
      });

      const data = (await response.json()) as RestartResponse;

      if (!response.ok) {
        setError(data.message || "Failed to restart service");
        setLoading(false);
        return null;
      }

      // Service is restarting, response should be successful
      return data;
    } catch (err) {
      // Connection error is expected after restart is initiated
      if (err instanceof Error) {
        console.log("Connection lost (expected during restart):", err.message);
        setError(`Connection lost (expected during restart): ${err.message}`);
      } else {
        console.log("Connection lost (expected during restart)");
        setError("Connection lost (expected during restart)");
      }
      return {
        success: true,
        message: "Service is restarting...",
        service_manager: "unknown",
      };
    } finally {
      // Keep loading state until we detect service is back
    }
  }, []);

  const waitForReconnection = useCallback(
    async (maxAttempts: number = 30): Promise<boolean> => {
      let attempts = 0;

      while (attempts < maxAttempts) {
        try {
          await new Promise((resolve) => setTimeout(resolve, 2000));

          const response = await fetch("/api/version", {
            method: "GET",
            cache: "no-cache",
          });

          if (response.ok) {
            setLoading(false);
            return true;
          }
        } catch (err) {
          // Service not yet available
          if (err instanceof Error) {
            console.warn(
              `Attempt ${attempts + 1}: Service not available yet - ${
                err.message
              }`,
            );
          } else {
            console.warn(`Attempt ${attempts + 1}: Service not available yet`);
          }
          attempts++;
        }
      }

      setLoading(false);
      setError("Service did not restart within expected time");
      return false;
    },
    [],
  );

  return {
    restart,
    getSystemInfo,
    waitForReconnection,
    loading,
    error,
  };
};
