export type TelegramAddressSource =
  | "telegram"
  | "mirror"
  | "cache"
  | "geoip"
  | "builtin";

export interface TelegramBridgeAddresses {
  source: TelegramAddressSource;
  total: number;
  v4: number;
  v6: number;
  updated_at?: string;
  last_attempt?: string;
  last_error?: string;
}

export interface TelegramBridgeListener {
  running: boolean;
  port: number;
  v4: boolean;
  v6: boolean;
  active: number;
  error?: string;
  v6_error?: string;
}

export interface TelegramBridgeTproxy {
  checked: boolean;
  available: boolean;
  missing: string[];
  packages: string[];
}

export interface TelegramBridgeLegacySet {
  id: string;
  name: string;
  enabled: boolean;
}

export interface TelegramBridgeStats {
  relayed: number;
  failed_open: number;
  dial_failed: number;
  dropped: number;
  last_relayed_at?: string;
}

export interface TelegramBridgeStatus {
  success: boolean;
  enabled: boolean;
  addresses: TelegramBridgeAddresses;
  listener: TelegramBridgeListener;
  rule_installed: boolean;
  tproxy: TelegramBridgeTproxy;
  skip_setup: boolean;
  queue_mode: string;
  ipv6_enabled: boolean;
  legacy_sets: TelegramBridgeLegacySet[];
  stats: TelegramBridgeStats;
}
