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
  removed?: string[];
}

export type GeodatFileType = "geosite" | "geoip";

export interface GeodatRemoveResult {
  success: boolean;
  message: string;
  removed: string[];
  kept: string[];
}

export interface EngineFailure {
  mode: "nfqueue" | "tun";
  error: string;
  retry_at: number;
  retries_left: number;
}

export interface SystemInfo {
  service_manager: string;
  os: string;
  arch: string;
  can_restart: boolean;
  is_docker: boolean;
  host_has_global_ipv6?: boolean;
  ipv6_bypasses_sets?: boolean;
  engine_failure?: EngineFailure;
}

export type AddressScope = "public" | "private" | "cgnat" | "ula" | "other";

export interface HostAddress {
  iface: string;
  ip: string;
  scope: AddressScope;
}

export type ExposeService =
  | "web_server"
  | "mtproto"
  | "mtproto_web_proxy"
  | "socks5";

export type ExposeBlockReason =
  | "no_auth"
  | "web_no_auth"
  | "open_relay"
  | "shared_port"
  | "loopback"
  | "invalid_bind"
  | "not_listening";

export interface ExposedPort {
  service: ExposeService;
  port: number;
  address?: string;
  v4: boolean;
  v6: boolean;
}

export interface ExposeBlock {
  service: ExposeService;
  reason: ExposeBlockReason;
}

export interface ExposureStatus {
  skip_setup: boolean;
  ports: ExposedPort[] | null;
  blocked: ExposeBlock[] | null;
  chains: string[] | null;
  error?: string;
}

export interface SystemAddresses {
  success: boolean;
  wan_v4?: HostAddress;
  wan_v6?: HostAddress;
  lan: HostAddress[] | null;
  public_v4?: string;
  public_error?: string;
  exposure: ExposureStatus;
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
