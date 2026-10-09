import { useEffect, useMemo, useState } from "react";
import { DeviceInfo, devicesApi } from "@b4.devices";
import { resolveDeviceName } from "@utils";

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
    return dev ? resolveDeviceName(dev) : mac;
  };

  return { devices, deviceMap, getDeviceName };
}
