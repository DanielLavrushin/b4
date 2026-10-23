import { useCallback, useMemo } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { DeviceInfo, devicesApi } from "@b4.devices";
import { resolveDeviceName } from "@utils";

const DEVICES_KEY = ["dashboard", "devices"] as const;
const NO_DEVICES: DeviceInfo[] = [];
const NO_ROUTER_IPS: string[] = [];

const macKey = (mac: string): string => mac.toUpperCase().replaceAll("-", ":");

export function useDeviceNames() {
  const queryClient = useQueryClient();
  const query = useQuery({ queryKey: DEVICES_KEY, queryFn: devicesApi.list });
  const devices = query.data?.devices ?? NO_DEVICES;
  const routerIps = query.data?.router_ips ?? NO_ROUTER_IPS;

  const deviceMap = useMemo(() => {
    const map: Record<string, DeviceInfo> = {};
    for (const d of devices) map[macKey(d.mac)] = d;
    return map;
  }, [devices]);

  const findDevice = (mac: string): DeviceInfo | undefined => deviceMap[macKey(mac)];

  const getDeviceName = (mac: string): string => {
    const dev = findDevice(mac);
    return dev ? resolveDeviceName(dev) : mac;
  };

  const refresh = useCallback(
    () => queryClient.invalidateQueries({ queryKey: DEVICES_KEY }),
    [queryClient],
  );

  return { devices, deviceMap, routerIps, findDevice, getDeviceName, refresh };
}
