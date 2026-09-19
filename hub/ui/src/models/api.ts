export type SetStatus = "pending" | "active" | "hidden" | "rejected";
export type MirrorStatus = "pending" | "approved" | "rejected";
export type LineageKind = "first" | "replaces" | "relists";

export interface SessionState {
  configured: boolean;
  authenticated: boolean;
  version: string;
}

export interface ApiErrorBody {
  code: string;
  error: string;
}

export interface TargetsView {
  domains: string[];
  ips: string[];
  geosite: string[];
  geoip: string[];
  summary: string;
}

export interface EmittedView {
  name: string;
  source: string;
}

export interface PinView {
  domain: string;
  addresses: string[];
}

export interface BlobRef {
  sha256: string;
  protocol: string;
  domain?: string;
  size: number;
}

export interface ReportView {
  id: number;
  set_id: string;
  version: number;
  key: string;
  key_hmac: string;
  asn_observed?: string;
  reason: string;
  received_at: string;
}

export interface VoteView {
  id: number;
  set_id: string;
  version: number;
  fp: string;
  key: string;
  key_hmac: string;
  kind: string;
  weight: number;
  asn_observed?: string;
  country_observed?: string;
  asn_hint?: string;
  country_hint?: string;
  origin_verified: boolean;
  domain?: string;
  b4_version?: string;
  engine?: string;
  received_at: string;
}

export interface VotesView {
  works: number;
  broken: number;
}

export interface LineageView {
  kind: LineageKind;
  current_version?: number;
}

export type Projection = Record<string, unknown>;

export interface EntryView {
  set_id: string;
  version: number;
  title: string;
  description?: string;
  family?: string;
  engine?: string;
  b4_version?: string;
  b4_min?: string;
  fp: string;
  status: SetStatus;
  status_reason?: string;
  flags: string[];
  author: string;
  uploader_hmac: string;
  asn_observed?: string;
  country_observed?: string;
  asn_hint?: string;
  country_hint?: string;
  created_at: string;
  updated_at: string;
  targets: TargetsView;
  strategy: string[];
  emitted: EmittedView[];
  pins: PinView[];
  doh_host?: string;
  payloads: BlobRef[];
  projection: Projection;
  decode_error?: string;
  reports: ReportView[];
  independent_reports: number;
  votes: VotesView;
  versions?: number[];
  superseded_by?: number;
  superseded_at?: string;
  lineage?: LineageView;
  edited_at?: string;
  edit_note?: string;
  original_projection?: Projection;
  original_title?: string;
  original_description?: string;
}

export type TidyKind = "dead_wildcard" | "covered" | "duplicate" | "www_only";

export interface SuggestionView {
  kind: TidyKind;
  entry: string;
  by?: string;
  replacement?: string;
}

export interface TidyView {
  suggestions: SuggestionView[];
  domains: string[];
}

export interface WarningView {
  code: string;
  params?: Record<string, unknown>;
}

export interface StrippedView {
  path: string;
  reason: string;
}

export interface DuplicateView {
  set_id: string;
  version: number;
  title: string;
  status: SetStatus;
}

export interface EditRequest {
  title: string;
  description: string;
  projection: Projection;
  note: string;
  approve: boolean;
  expect_updated_at?: string;
}

export interface EditPreview {
  title: string;
  description: string;
  projection: Projection;
  payloads: BlobRef[];
  warnings: WarningView[];
  stripped: StrippedView[];
  fp: string;
  fp_changed: boolean;
  changed: boolean;
  targets: TargetsView;
  strategy: string[];
  flags: string[];
  family: string;
  b4_min: string;
  duplicate?: DuplicateView;
  tidy?: TidyView;
}

export interface SetsView {
  pending: EntryView[];
  listed: EntryView[];
  superseded: EntryView[];
  hidden: EntryView[];
  rejected: EntryView[];
}

export type SetGroup = keyof SetsView;

export interface SetDetailView {
  id: string;
  author: string;
  author_hmac: string;
  current_version: number;
  derived_from_id?: string;
  derived_from_version?: number;
  created_at: string;
  updated_at: string;
  versions: EntryView[];
  votes: VoteView[];
}

export interface KeyView {
  key_hmac: string;
  label: string;
  first_seen: string;
  banned: boolean;
  ban_reason?: string;
  banned_at?: string;
  trusted: boolean;
  trusted_at?: string;
  sets: number;
  votes: number;
  reports: number;
}

export interface MirrorView {
  id: number;
  url: string;
  key_hmac: string;
  key: string;
  status: MirrorStatus;
  healthy: boolean;
  first_seen: string;
  last_seen: string;
  last_check?: string;
  last_ok?: string;
  reason?: string;
  version?: string;
}

export interface FeedbackView {
  votes: VoteView[];
  reports: ReportView[];
}

export interface GeoFileStatus {
  name: string;
  url: string;
  final_url?: string;
  sha256?: string;
  size?: number;
  fetched_at?: string;
  error?: string;
}

export interface CatalogueView {
  published: boolean;
  file?: string;
  size?: number;
  epoch: number;
  seq: number;
  generated_at?: string;
  expires_at?: string;
  sets: number;
  blobs: number;
  mirrors: string[];
  revoked_keys: string[];
  built_at?: string;
  dirty: boolean;
}

export interface CountsView {
  pending: number;
  listed: number;
  superseded: number;
  hidden: number;
  rejected: number;
  keys: number;
  banned: number;
  mirrors_pending: number;
  mirrors_approved: number;
  mirrors_rejected: number;
  votes: number;
  reports: number;
}

export interface OverviewView {
  version: string;
  source?: string;
  key_id: string;
  public_url?: string;
  now: string;
  catalogue: CatalogueView;
  counts: CountsView;
  geo: GeoFileStatus[];
}

export interface ActionResult {
  notice: string;
}

export type SetAction = "approve" | "reject" | "hide";
export type KeyAction = "ban" | "unban" | "trust" | "untrust";
export type MirrorAction = "approve" | "reject" | "remove";

export interface LimitsView {
  shares_per_day: number;
  votes_per_day: number;
  reports_per_day: number;
  mirrors_per_day: number;
  new_keys_per_day: number;
  requests_per_hour: number;
}

export interface SettingsView {
  limits: LimitsView;
  defaults: LimitsView;
}
