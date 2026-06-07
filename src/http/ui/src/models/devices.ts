import { B4Config, SettingsPropHandlerType } from "@b4.settings";

export interface DeviceInfo {
  mac: string;
  ip: string;
  hostname: string;
  vendor: string;
  alias?: string;
  country: string;
  is_manual?: boolean;
}

export interface DevicesResponse {
  available: boolean;
  source?: string;
  devices: DeviceInfo[];
  router_ips?: string[];
}

export interface DevicesSettingsProps {
  config: B4Config;
  onChange: (field: string, value: SettingsPropHandlerType) => void;
}
