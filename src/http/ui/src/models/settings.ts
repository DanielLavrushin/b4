import { B4SetConfig } from "@b4.sets";
import { Device, MTProtoSecret } from "./config";

export type SettingsPropHandlerType =
  | string
  | number
  | boolean
  | string[]
  | B4SetConfig[]
  | Device[]
  | MTProtoSecret[]
  | null
  | undefined;

export interface GeodatSource {
  name: string;
  geosite_url: string;
  geoip_url: string;
}

export interface GeoFileInfo {
  exists: boolean;
  size?: number;
  last_modified?: string;
}

export interface GeodatDownloadResult {
  success: boolean;
  message: string;
  geosite_path: string;
  geoip_path: string;
  geosite_size: number;
  geoip_size: number;
}

export interface SystemInfo {
  service_manager: string;
  os: string;
  arch: string;
  can_restart: boolean;
  is_docker: boolean;
}

export interface RestartResponse {
  success: boolean;
  message: string;
  service_manager: string;
  restart_command?: string;
}

export interface UpdateResponse {
  success: boolean;
  message: string;
  service_manager: string;
  update_command?: string;
}
