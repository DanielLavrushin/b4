export type SetStatus = "pending" | "active" | "hidden" | "rejected";

export type MirrorStatus = "pending" | "approved" | "rejected";

export type LineageKind = "first" | "replaces" | "older" | "relists";

export type TidyKind = "dead_wildcard" | "covered" | "duplicate" | "www_only";

export type SetAction = "approve" | "reject" | "hide" | "restore";

export type KeyAction = "ban" | "unban" | "trust" | "untrust";

export type MirrorAction = "approve" | "reject" | "remove";

export type ReportAction = "dismiss" | "resolve" | "reopen";

export type ReportState = "open" | "dismissed" | "resolved";

export type BuildState = "idle" | "queued" | "building";

export type MirrorLag = "unknown" | "current" | "behind" | "stale" | "ahead";

export type WithheldReason = "set_withdrawn" | "author_banned";

export type SimilarRelation = "same_targets" | "same_strategy" | "shared_targets" | "covering_domains" | "same_title" | "similar_title" | "same_author" | "same_set";

export type AttentionCode = "low_score" | "stale" | "reports" | "weak";

export type TechniqueCode = "fake" | "sni_mutation" | "tls_mod" | "frag" | "syn_fake" | "desync" | "incoming" | "window" | "duplicate" | "rst_protection" | "drop_sack" | "http_method_eol" | "seg2_delay" | "udp" | "dns" | "block" | "mss_clamp" | "passthrough";

export type FilterCode = "tls_only" | "ip_only" | "domain_only";

export type SetGroupName = "pending" | "listed" | "superseded" | "withheld" | "hidden" | "rejected";

export type AppliedState = "counted" | "not_published" | "after_build" | "excluded" | "capped";

export type KeyTag = "staff" | "test";

export type EmitSource = "builtin_payload" | "capture" | "payload_domain" | "custom_payload" | "fake_snis" | "quic_capture";

export type ReasonScope = "reject" | "hide" | "withdraw" | "ban" | "mirror_reject" | "report_dismiss";

export type IssueCode = "not_published" | "build_failed" | "build_overdue" | "manifest_expiring" | "queue_old" | "mirror_failing" | "mirror_stale" | "mirror_pending" | "reports_open" | "dev_build" | "dirty_source";

export type Severity = "error" | "warning" | "info";

export type NotifyEvent = "share" | "report" | "auto_hide" | "build_failed" | "mirror";

export type NotifyChannel = "telegram" | "webhook";

export type NotifyLanguage = "en" | "ru";

export interface ApiErrorBody {
  code: string;
  error: string;
  params?: Record<string, unknown>;
  items?: ModerationItemView[];
}

export interface SessionState {
  configured: boolean;
  authenticated: boolean;
  version: string;
}

export interface OverviewView {
  version: string;
  source?: string;
  key_id: string;
  public_url?: string;
  now: string;
  catalogue: CatalogueView;
  build: BuildStateView;
  counts: CountsView;
}

export interface SetsView {
  pending: EntryView[];
  listed: EntryView[];
  superseded: EntryView[];
  withheld: EntryView[];
  hidden: EntryView[];
  rejected: EntryView[];
}

export interface SetDetailView {
  id: string;
  author: string;
  author_hmac: string;
  current_version: number;
  derived_from_id?: string;
  derived_from_version?: number;
  created_at: string;
  updated_at: string;
  withdrawn_at?: string;
  withdraw_reason?: string;
  withheld?: WithheldReason;
  author_banned?: boolean;
  listed_version?: number;
  versions: EntryView[];
  votes: VoteView[];
}

export interface EditRequest {
  title: string;
  description: string;
  projection: Record<string, unknown>;
  note: string;
  approve: boolean;
  expect_updated_at?: string;
}

export interface EditPreview {
  title: string;
  description: string;
  projection: Record<string, unknown>;
  payloads: BlobRef[];
  warnings: WarningView[];
  stripped: StrippedView[];
  fp: string;
  fp_changed: boolean;
  changed: boolean;
  targets: TargetsView;
  strategy: Term[];
  flags: string[];
  family: string;
  b4_min: string;
  duplicate?: DuplicateView;
  tidy?: TidyView;
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
  check_code?: string;
  check_error?: string;
  check_ms?: number;
  served_epoch?: number;
  served_seq?: number;
  served_generated_at?: string;
  announced: boolean;
  announce_next: boolean;
  kept?: boolean;
  drops_at?: string;
  lag: MirrorLag;
  behind_by?: number;
}

export interface FeedbackView {
  votes: VoteView[];
  reports: ReportView[];
}

export interface SettingsView {
  limits: LimitsView;
  defaults: LimitsView;
}

export interface ActionResult {
  notice: string;
  code: string;
  params?: Record<string, unknown>;
  build?: BuildStateView;
}

export interface ModerationRequest {
  action: SetAction;
  reason: string;
  items: ModerationItemRequest[];
  force: boolean;
  withdraw: boolean;
  keep_reports: boolean;
  partial: boolean;
  dry_run: boolean;
}

export interface ModerationView {
  notice: string;
  code: string;
  params?: Record<string, unknown>;
  batch_id?: string;
  items: ModerationItemView[];
  build?: BuildStateView;
}

export interface KeyImpactView {
  listed: SetRefView[];
  pending: SetRefView[];
  votes: number;
  voted_sets: number;
  reports: number;
  mirrors: MirrorView[];
}

export interface MirrorsView {
  manifest: MirrorsManifestView;
  check_interval_s: number;
  window_s: number;
  mirrors: MirrorView[];
  orphans: string[];
}

export interface MirrorCheckResult {
  notice: string;
  code: string;
  params?: Record<string, unknown>;
  build?: BuildStateView;
  mirror: MirrorView;
}

export interface ReportsPageView {
  items: ReportView[];
  total: number;
  next?: string;
  counts: Record<string, number>;
}

export interface ReportsActionRequest {
  ids: number[];
  action: ReportAction;
  note: string;
}

export interface AuditPageView {
  items: AuditEntryView[];
  next?: number;
}

export interface BuildsPageView {
  items: BuildRunView[];
  next?: number;
}

export interface SimilarView {
  set_id: string;
  version: number;
  scanned: number;
  truncated: boolean;
  items: SimilarItemView[];
}

export interface BadgesView {
  pending: number;
  oldest_pending?: string;
  reports_open: number;
  mirrors_pending: number;
  attention: number;
  build: BuildStateView;
}

export interface SetRowsView {
  group: SetGroupName;
  total: number;
  groups: Record<string, number>;
  attention: number;
  rows: SetRowView[];
}

export interface QueueView {
  items: EntryView[];
}

export interface KeyListView {
  items: KeyRowView[];
  total: number;
  counts: KeyCountsView;
  match_by?: string;
}

export interface KeyDetailView {
  key: KeyRowView;
  kinds: ActivityKindView[];
  days: ActivityDayView[];
  sets: KeySetView[];
  votes: VoteRowView[];
  votes_total: number;
  reports: ReportView[];
  origins: KeyOriginView[];
  clients: KeyClientView[];
  mirrors: MirrorView[];
  history: AuditEntryView[];
}

export interface KeyProfileRequest {
  name: string;
  note: string;
  tag: string;
}

export interface NotableKeyView {
  key_hmac: string;
  name?: string;
  tag?: KeyTag;
  banned?: boolean;
  trusted?: boolean;
}

export interface VotesPageView {
  items: VoteRowView[];
  total: number;
  next?: string;
}

export interface VoteOriginsView {
  asns: VoteASNView[];
  countries: MixView[];
}

export interface GeoCategoriesView {
  geosite: string[];
  geoip: string[];
}

export interface InvalidFieldView {
  path: string;
  code: string;
  message: string;
  params?: Record<string, unknown>;
}

export interface ReasonPresetView {
  id: number;
  scope: ReasonScope;
  label?: string;
  text: string;
  position: number;
  uses: number;
  last_used?: string;
  created_at: string;
}

export interface ReasonPresetRequest {
  scope: ReasonScope;
  label: string;
  text: string;
  position: number;
}

export interface ReasonMoveRequest {
  delta: number;
}

export interface TextEditRequest {
  title: string;
  description: string;
  note: string;
  expect?: string;
}

export interface HealthView {
  now: string;
  worst: string;
  issues: IssueView[];
  manifest: HealthManifestView;
  build: BuildStateView;
  pending: HealthPendingView;
  reports_open: number;
  mirrors: HealthMirrorsView;
  build_info: BuildInfoView;
}

export interface StatsView {
  from: string;
  to: string;
  days: number;
  daily: StatsDayView[];
  totals: StatsDayView;
  coverage: CoverageView;
  scores: ScoreStatsView;
  countries: MixView[];
  asns: MixView[];
  clients: MixView[];
  top_sets: TopSetView[];
}

export interface NotifyView {
  telegram: NotifyTelegramView;
  webhook: NotifyWebhookView;
  events: NotifyEvent[];
  available: NotifyEvent[];
  digest_seconds: number;
  digest_min: number;
  digest_max: number;
  language: NotifyLanguage;
  status: Record<string, NotifyChannelStatusView>;
}

export interface NotifyRequest {
  telegram: NotifyTelegramRequest;
  webhook: NotifyWebhookRequest;
  events: NotifyEvent[];
  digest_seconds: number;
  language: NotifyLanguage;
}

export interface NotifyTestRequest {
  channel: NotifyChannel;
}

export interface ModerationItemView {
  set_id: string;
  version: number;
  title?: string;
  from?: SetStatus;
  to?: SetStatus;
  listed: number;
  listed_after: number;
  withheld?: WithheldReason;
  reports: number;
  ok: boolean;
  code?: string;
  params?: Record<string, unknown>;
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
  announced_mirrors: string[];
  hub_listed: boolean;
  revoked_keys: string[];
  signing_key: string;
  builtin_keys: string[];
  built_at?: string;
  dirty: boolean;
}

export interface BuildStateView {
  state: BuildState;
  trigger?: string;
  queued_at?: string;
  started_at?: string;
  dirty: boolean;
  last_ok?: BuildRunView;
  last_error?: BuildRunView;
}

export interface CountsView {
  pending: number;
  listed: number;
  superseded: number;
  withheld: number;
  hidden: number;
  rejected: number;
  reports_open: number;
  keys: number;
  banned: number;
  mirrors_pending: number;
  mirrors_approved: number;
  mirrors_rejected: number;
  votes: number;
  reports: number;
}

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
  strategy: Term[];
  emitted: EmittedView[];
  pins: PinView[];
  doh_host?: string;
  payloads: BlobRef[];
  projection: Record<string, unknown>;
  config?: Record<string, unknown>;
  decode_error?: string;
  reports: ReportView[];
  independent_reports: number;
  open_reports: number;
  author_banned?: boolean;
  withheld?: WithheldReason;
  hidden_from?: SetStatus;
  votes: VotesView;
  versions?: number[];
  superseded_by?: number;
  superseded_at?: string;
  lineage?: LineageView;
  edited_at?: string;
  edit_note?: string;
  original_projection?: Record<string, unknown>;
  original_title?: string;
  original_description?: string;
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

export interface BlobRef {
  sha256: string;
  protocol: string;
  domain?: string;
  size: number;
}

export interface WarningView {
  code: string;
  params?: Record<string, unknown>;
}

export interface StrippedView {
  path: string;
  reason: string;
}

export interface TargetsView {
  domains: string[];
  ips: string[];
  geosite: string[];
  geoip: string[];
  asns: string[];
  filters: Term[];
}

export interface Term {
  code: string;
  params?: Record<string, unknown>;
}

export interface DuplicateView {
  set_id: string;
  version: number;
  title: string;
  status: SetStatus;
}

export interface TidyView {
  suggestions: SuggestionView[];
  domains: string[];
}

export interface ReportView {
  id: number;
  set_id: string;
  version: number;
  title?: string;
  set_status?: SetStatus;
  key: string;
  key_hmac: string;
  key_banned?: boolean;
  key_test?: boolean;
  asn_observed?: string;
  reason: string;
  received_at: string;
  state: ReportState;
  resolution?: string;
  note?: string;
  resolved_at?: string;
  counts: boolean;
}

export interface LimitsView {
  shares_per_day: number;
  votes_per_day: number;
  reports_per_day: number;
  mirrors_per_day: number;
  new_keys_per_day: number;
  requests_per_hour: number;
}

export interface ModerationItemRequest {
  set_id: string;
  version: number;
  expect_status?: string;
}

export interface SetRefView {
  set_id: string;
  version: number;
  title: string;
}

export interface MirrorsManifestView {
  published: boolean;
  epoch: number;
  seq: number;
  generated_at?: string;
  hub_url?: string;
  listed: string[];
}

export interface AuditEntryView {
  id: number;
  at: string;
  actor: string;
  actor_ref?: string;
  actor_ip?: string;
  action: string;
  target_kind: string;
  target_id?: string;
  target_label?: string;
  version?: number;
  reason?: string;
  before?: Record<string, unknown>;
  after?: Record<string, unknown>;
  batch_id?: string;
}

export interface BuildRunView {
  id: number;
  trigger: string;
  started_at: string;
  finished_at?: string;
  ok: boolean;
  error?: string;
  epoch?: number;
  seq?: number;
  file?: string;
  size?: number;
  sets: number;
  blobs: number;
  mirrors: number;
  duration_ms: number;
  changes: BuildChanges;
  content_changed: boolean;
}

export interface SimilarItemView {
  set_id: string;
  version: number;
  title: string;
  status: SetStatus;
  status_reason?: string;
  listed: boolean;
  score: number;
  relations: SimilarRelationView[];
  shared: string[];
  votes: VotesView;
  updated_at: string;
}

export interface SetRowView {
  set_id: string;
  version: number;
  title: string;
  status: SetStatus;
  status_reason?: string;
  family?: string;
  flags: string[];
  techniques: Term[];
  targets: TargetsBriefView;
  author_hmac: string;
  author: string;
  author_banned?: boolean;
  asn_observed?: string;
  country_observed?: string;
  published?: ScoreView;
  live: ScoreView;
  evidence: EvidenceView;
  attention: AttentionCode[];
  reports: number;
  open_reports: number;
  independent_reports: number;
  versions?: number[];
  superseded_by?: number;
  superseded_at?: string;
  withheld?: WithheldReason;
  hidden_from?: SetStatus;
  config?: Record<string, unknown>;
  decode_error?: boolean;
  created_at: string;
  updated_at: string;
  edited_at?: string;
}

export interface KeyRowView {
  key_hmac: string;
  label: string;
  name?: string;
  note?: string;
  tag?: KeyTag;
  first_seen: string;
  last_seen?: string;
  banned: boolean;
  ban_reason?: string;
  banned_at?: string;
  trusted: boolean;
  trusted_at?: string;
  profile_updated_at?: string;
  records: number;
  sets: number;
  listed_sets: number;
  pending_versions: number;
  votes: number;
  manual_votes: number;
  reports: number;
  reports_against: number;
  last_asn?: string;
  last_country?: string;
}

export interface KeyCountsView {
  all: number;
  banned: number;
  trusted: number;
  staff: number;
  test: number;
  with_sets: number;
  active_7d: number;
}

export interface ActivityKindView {
  kind: string;
  count: number;
  first: string;
  last: string;
}

export interface ActivityDayView {
  day: string;
  share: number;
  vote: number;
  report: number;
  mirror: number;
}

export interface KeySetView {
  set_id: string;
  title: string;
  listed_version?: number;
  withheld?: WithheldReason;
  versions: KeySetVersionView[];
  score?: ScoreView;
  reports: number;
}

export interface VoteRowView {
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
  title?: string;
  set_status?: SetStatus;
  author_vote?: boolean;
  counted_in: string[];
  applied: AppliedView;
}

export interface KeyOriginView {
  asn: string;
  name?: string;
  country?: string;
  count: number;
  first: string;
  last: string;
  sources: string[];
}

export interface KeyClientView {
  b4_version: string;
  engine?: string;
  count: number;
  last: string;
}

export interface VoteASNView {
  key: string;
  name?: string;
  country?: string;
  votes: number;
  keys: number;
  countries: string[];
}

export interface MixView {
  key: string;
  name?: string;
  country?: string;
  votes: number;
  keys: number;
}

export interface IssueView {
  code: IssueCode;
  severity: Severity;
  params?: Record<string, unknown>;
}

export interface HealthManifestView {
  published: boolean;
  epoch?: number;
  seq?: number;
  generated_at?: string;
  expires_at?: string;
  expires_in_s?: number;
}

export interface HealthPendingView {
  count: number;
  oldest?: SetRefView;
  since?: string;
}

export interface HealthMirrorsView {
  approved: number;
  announced: number;
  failing: number;
  stale: number;
  pending: number;
}

export interface BuildInfoView {
  version: string;
  source?: string;
  dev: boolean;
  dirty_source: boolean;
}

export interface StatsDayView {
  day: string;
  shares: number;
  duplicates: number;
  works: number;
  broken: number;
  reports: number;
  new_keys: number;
  mirrors: number;
  approved: number;
  rejected: number;
  hidden: number;
  withdrawn: number;
  auto_hidden: number;
  builds: number;
  build_failed: number;
}

export interface CoverageView {
  votes: number;
  unverified: number;
  devices: number;
}

export interface ScoreStatsView {
  listed: number;
  rated: number;
  median: number;
  bins: ScoreBinView[];
  low_score: number;
  stale: number;
}

export interface TopSetView {
  set_id: string;
  title: string;
  status?: string;
  votes: number;
  keys: number;
  works: number;
  broken: number;
}

export interface NotifyTelegramView {
  enabled: boolean;
  chat_id?: string;
  chat_from_env: boolean;
  token_set: boolean;
  token_hint?: string;
  token_from_env: boolean;
}

export interface NotifyWebhookView {
  enabled: boolean;
  url_set: boolean;
  url_hint?: string;
  url_from_env: boolean;
  secret_set: boolean;
  secret_from_env: boolean;
}

export interface NotifyChannelStatusView {
  last_ok_at?: string;
  last_error?: string;
  last_error_at?: string;
  failures: number;
  sent_total: number;
}

export interface NotifyTelegramRequest {
  enabled: boolean;
  chat_id: string;
  token?: string;
  clear_token?: boolean;
}

export interface NotifyWebhookRequest {
  enabled: boolean;
  url?: string;
  clear_url?: boolean;
  secret?: string;
  clear_secret?: boolean;
}

export interface EmittedView {
  name: string;
  source: EmitSource;
  unreadable?: boolean;
}

export interface PinView {
  domain: string;
  addresses: string[];
}

export interface VotesView {
  works: number;
  broken: number;
}

export interface LineageView {
  kind: LineageKind;
  current_version?: number;
}

export interface SuggestionView {
  kind: TidyKind;
  entry: string;
  by?: string;
  replacement?: string;
}

export interface BuildChanges {
  added?: BuildSetRef[];
  removed?: BuildSetRef[];
  updated?: BuildVersionChange[];
  edited?: BuildSetRef[];
  rescored?: number;
  mirrors_added?: string[];
  mirrors_removed?: string[];
  revoked_added?: string[];
}

export interface SimilarRelationView {
  code: SimilarRelation;
  count?: number;
}

export interface TargetsBriefView {
  domains: string[];
  domains_total: number;
  ips: number;
  geosite: string[];
  geoip: string[];
  asns: string[];
  filters: Term[];
}

export interface ScoreView {
  score: number;
  n: number;
  devices: number;
  newest?: string;
  bucket: string;
}

export interface EvidenceView {
  independent: EvidenceSideView;
  author: EvidenceSideView;
  pooled: number;
}

export interface KeySetVersionView {
  version: number;
  status: SetStatus;
}

export interface AppliedView {
  at?: string;
  base: number;
  origin: number;
  young_key: number;
  decay: number;
  weight: number;
  state: AppliedState;
  reason?: string;
}

export interface ScoreBinView {
  lo: number;
  hi: number;
  sets: number;
  low_n: number;
}

export interface BuildSetRef {
  set_id: string;
  version: number;
  title: string;
}

export interface BuildVersionChange {
  set_id: string;
  title: string;
  from: number;
  to: number;
}

export interface EvidenceSideView {
  works: number;
  broken: number;
  devices: number;
  positive: number;
  negative: number;
}
