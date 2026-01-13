export type FakingStrategy =
  | "ttl"
  | "pastseq"
  | "randseq"
  | "tcp_check"
  | "md5sum";

export enum FakingPayloadType {
  RANDOM = 0,
  CUSTOM = 1,
  DEFAULT = 2,
  DEFAULT2 = 3,
  CAPTURE = 4,
}

export type MutationMode =
  | "off"
  | "random"
  | "grease"
  | "padding"
  | "fakeext"
  | "fakesni"
  | "advanced";
export interface SNIMutationConfig {
  mode: MutationMode;
  grease_count: number;
  padding_size: number;
  fake_ext_count: number;
  fake_snis: string[];
}

export interface FakingConfig {
  strategy: FakingStrategy;
  sni: boolean;
  ttl: number;
  seq_offset: number;
  sni_seq_length: number;
  sni_type: FakingPayloadType;
  custom_payload: string;
  sni_mutation: SNIMutationConfig;
  payload_file: string;
  tls_mod: string[];
  tcp_md5: boolean;
}
export type FragmentationStrategy =
  | "tcp"
  | "ip"
  | "tls"
  | "oob"
  | "disorder"
  | "extsplit"
  | "firstbyte"
  | "combo"
  | "hybrid"
  | "none";
export interface FragmentationConfig {
  strategy: FragmentationStrategy;
  sni_position: number;
  reverse_order: boolean;
  middle_sni: boolean;
  oob_position: number;
  oob_char: number;

  tlsrec_pos: number;

  seq_overlap_pattern: string[];

  combo: ComboFragConfig;
  disorder: DisorderFragConfig;
}

export enum LogLevel {
  ERROR = 0,
  INFO = 1,
  TRACE = 2,
  DEBUG = 3,
}

export interface LoggingConfig {
  level: LogLevel;
  instaflush: boolean;
  syslog: boolean;
  error_file: string;
}

export interface TargetsConfig {
  sni_domains: string[];
  ip: string[];
  geosite_categories: string[];
  geoip_categories: string[];
}

export interface DomainStatisticsConfig {
  manual_domains: number;
  geosite_domains: number;
  total_domains: number;
  category_breakdown?: Record<string, number>;
  geosite_available: boolean;
}

export interface CategoryPreviewConfig {
  category: string;
  total_domains: number;
  preview_count: number;
  preview: string[];
}

export type UdpMode = "drop" | "fake";
export type UdpFilterQuicMode = "disabled" | "all" | "parse";
export type UdpFakingStrategy = "none" | "ttl" | "checksum";

export interface UdpConfig {
  mode: UdpMode;
  fake_seq_length: number;
  fake_len: number;
  faking_strategy: UdpFakingStrategy;
  dport_filter: string;
  filter_quic: UdpFilterQuicMode;
  conn_bytes_limit: number;
  filter_stun: boolean;
  seg2delay: number;
}
export interface QueueConfig {
  start_num: number;
  threads: number;
  mark: number;
  ipv4: boolean;
  ipv6: boolean;
  interfaces: string[];
  devices: DevicesConfig;
}

export interface DevicesConfig {
  mac: string[];
  enabled: boolean;
  wisb: boolean;
}

export interface DiscoveryConfig {
  discovery_timeout: number;
  config_propagate_ms: number;
  reference_domain: string;
  reference_dns: string[];
}

export type WindowMode = "off" | "oscillate" | "zero" | "random" | "escalate";
export type DesyncMode = "off" | "rst" | "fin" | "ack" | "combo" | "full";
export type IncomingMode = "off" | "fake" | "reset" | "fin" | "desync";
export type IncomingStrategy = "badsum" | "badseq" | "badack" | "rand" | "all";

export interface TcpConfig {
  conn_bytes_limit: number;
  seg2delay: number;
  syn_fake: boolean;
  syn_fake_len: number;
  syn_ttl: number;
  drop_sack: boolean;

  desync: DesyncConfig;
  win: WinConfig;
  incoming: IncomingConfig;
}

export interface IncomingConfig {
  mode: IncomingMode;
  min: number;
  max: number;
  fake_ttl: number;
  fake_count: number;
  strategy: IncomingStrategy;
}

export interface WinConfig {
  mode: WindowMode;
  values: number[];
}

export interface DesyncConfig {
  mode: DesyncMode;
  ttl: number;
  count: number;
  post_desync: boolean;
}

export interface WebServerConfig {
  port: number;
  bind_address: string;
}
export interface TableConfig {
  monitor_interval: number;
  skip_setup: false;
}

export interface GeoConfig {
  sitedat_url: string;
  ipdat_url: string;
  sitedat_path: string;
  ipdat_path: string;
}

export interface ApiConfig {
  ipinfo_token: string;
}

export interface SystemConfig {
  logging: LoggingConfig;
  web_server: WebServerConfig;
  tables: TableConfig;
  checker: DiscoveryConfig;
  geo: GeoConfig;
  api: ApiConfig;
}

export interface B4Config {
  queue: QueueConfig;
  system: SystemConfig;
  sets: B4SetConfig[];
  available_ifaces: string[];
}

export interface B4SetConfig {
  id: string;
  name: string;
  enabled: boolean;

  tcp: TcpConfig;
  udp: UdpConfig;
  fragmentation: FragmentationConfig;
  faking: FakingConfig;
  targets: TargetsConfig;
  dns: DNSConfig;
}

export type ComboShuffleMode = "middle" | "full" | "reverse";
export interface ComboFragConfig {
  first_byte_split: boolean;
  extension_split: boolean;
  shuffle_mode: ComboShuffleMode;
  first_delay_ms: number;
  jitter_max_us: number;
  decoy_enabled: boolean;
  decoy_snis: string[];
}

export type DisorderShuffleMode = "full" | "reverse";
export interface DisorderFragConfig {
  shuffle_mode: DisorderShuffleMode;
  min_jitter_us: number;
  max_jitter_us: number;
}

export interface DNSConfig {
  enabled: boolean;
  target_dns: string;
  fragment_query: boolean;
}

export const MAIN_SET_ID = "11111111-1111-1111-1111-111111111111";
export const NEW_SET_ID = "00000000-0000-0000-0000-000000000000";
