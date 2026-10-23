import type { ReactNode } from "react";
import { Box } from "@mui/material";
import { useTranslation } from "react-i18next";
import { fonts } from "@design";
import type { DeviceInfo } from "@b4.devices";
import { resolveDeviceName } from "@utils";

export interface DeviceLabel {
  name: string;
  meta?: string;
  tip?: ReactNode;
}

export function DeviceTip({ mac, vendor }: { mac: string; vendor: string }) {
  const { t } = useTranslation();
  return (
    <Box sx={{ fontSize: 12, lineHeight: 1.5 }}>
      <Box sx={{ fontFamily: fonts.mono, fontSize: 11 }}>
        {t("dashboard.blocked.mac", { value: mac })}
      </Box>
      {vendor && <Box>{vendor}</Box>}
    </Box>
  );
}

export function macDeviceLabel(
  mac: string,
  device: DeviceInfo | undefined,
  lastIp = "",
): DeviceLabel {
  const ip = device?.ip || lastIp;
  const name = device ? resolveDeviceName({ ...device, ip }) : ip || mac;
  const vendor =
    device?.vendor && device.vendor !== "Private" && !name.includes(device.vendor)
      ? device.vendor
      : "";
  const named = Boolean(device?.alias || device?.hostname);
  return {
    name,
    meta: (named && ip) || undefined,
    tip: name.includes(mac) ? undefined : <DeviceTip mac={mac} vendor={vendor} />,
  };
}

export const deviceTrafficLink = (key: string): string =>
  `/traffic?device=${encodeURIComponent(key)}`;
