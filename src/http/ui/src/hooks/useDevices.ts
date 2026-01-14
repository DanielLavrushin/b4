import { useState, useCallback } from "react";
import { ApiResponse } from "@api/apiClient";
import { DeviceInfo, devicesApi } from "@b4.devices";

export function useDevices() {
  const [devices, setDevices] = useState<DeviceInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [available, setAvailable] = useState(false);
  const [source, setSource] = useState<string>("");

  const loadDevices = useCallback(async () => {
    setLoading(true);
    try {
      const data = await devicesApi.list();
      setAvailable(data.available);
      setSource(data.source || "");
      setDevices(data.devices || []);
      return data;
    } catch (err) {
      console.error("Failed to load devices:", err);
      return null;
    } finally {
      setLoading(false);
    }
  }, []);

  const setAlias = useCallback(
    async (mac: string, alias: string): Promise<ApiResponse<void>> => {
      try {
        await devicesApi.setAlias(mac, alias);
        setDevices((prev) =>
          prev.map((d) => (d.mac === mac ? { ...d, alias } : d)),
        );
        return { success: true };
      } catch (e) {
        return { success: false, error: String(e) };
      }
    },
    [],
  );

  const resetAlias = useCallback(
    async (mac: string): Promise<ApiResponse<void>> => {
      try {
        await devicesApi.resetAlias(mac);
        setDevices((prev) =>
          prev.map((d) => (d.mac === mac ? { ...d, alias: undefined } : d)),
        );
        return { success: true };
      } catch (e) {
        return { success: false, error: String(e) };
      }
    },
    [],
  );

  return {
    devices,
    loading,
    available,
    source,
    loadDevices,
    setAlias,
    resetAlias,
  };
}
