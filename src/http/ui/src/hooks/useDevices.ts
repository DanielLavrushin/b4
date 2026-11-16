import { useState, useCallback } from "react";
import { devicesApi } from "@api/devices";
import { DeviceInfo } from "@models/devices";

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

  return {
    devices,
    loading,
    available,
    source,
    loadDevices,
  };
}
