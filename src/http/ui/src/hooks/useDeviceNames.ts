import { useEffect, useMemo, useState } from "react";
import { DeviceInfo, devicesApi } from "@b4.devices";

export function useDeviceNames() {
  const [devices, setDevices] = useState<DeviceInfo[]>([]);

  useEffect(() => {
    devicesApi
      .list()
      .then((data) => setDevices(data.devices || []))
      .catch(() => {});
  }, []);

  const deviceMap = useMemo(() => {
    const map: Record<string, DeviceInfo> = {};
    for (const d of devices) map[d.mac] = d;
    return map;
  }, [devices]);

  const getDeviceName = (mac: string): string => {
    const dev = deviceMap[mac];
    if (dev?.alias) return dev.alias;
    if (dev?.hostname) return dev.hostname;
    if (dev?.vendor && dev.vendor !== "Private") return `${dev.vendor} (${mac})`;
    return mac;
  };

  const getDeviceMeta = (mac: string): string => {
    const dev = deviceMap[mac];
    if (!dev) return "";
    const parts: string[] = [];
    if (dev.ip) parts.push(dev.ip);
    if (dev.vendor && dev.vendor !== "Private") parts.push(dev.vendor);
    return parts.join(" · ");
  };

  return { devices, deviceMap, getDeviceName, getDeviceMeta };
}
