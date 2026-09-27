package store

var migrations = []string{
	`
CREATE TABLE meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE keys (
	key_hmac   TEXT PRIMARY KEY,
	first_seen TEXT NOT NULL,
	banned     INTEGER NOT NULL DEFAULT 0,
	ban_reason TEXT NOT NULL DEFAULT '',
	banned_at  TEXT NOT NULL DEFAULT ''
);

CREATE TABLE records (
	id          TEXT PRIMARY KEY,
	kind        TEXT NOT NULL,
	key_hmac    TEXT NOT NULL,
	set_id      TEXT NOT NULL DEFAULT '',
	version     INTEGER NOT NULL DEFAULT 0,
	received_at TEXT NOT NULL
);

CREATE TABLE sets (
	id                   TEXT PRIMARY KEY,
	author_hmac          TEXT NOT NULL,
	current_version      INTEGER NOT NULL DEFAULT 0,
	derived_from_id      TEXT NOT NULL DEFAULT '',
	derived_from_version INTEGER NOT NULL DEFAULT 0,
	created_at           TEXT NOT NULL,
	updated_at           TEXT NOT NULL
);
CREATE INDEX sets_author ON sets(author_hmac);

CREATE TABLE set_versions (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	set_id           TEXT NOT NULL REFERENCES sets(id),
	version          INTEGER NOT NULL,
	fp               TEXT NOT NULL,
	targets_key      TEXT NOT NULL,
	title            TEXT NOT NULL,
	description      TEXT NOT NULL DEFAULT '',
	projection_json  TEXT NOT NULL,
	payloads_json    TEXT NOT NULL DEFAULT '[]',
	flags_json       TEXT NOT NULL DEFAULT '[]',
	geo_json         TEXT NOT NULL DEFAULT '',
	b4_min           TEXT NOT NULL DEFAULT '',
	b4_version       TEXT NOT NULL DEFAULT '',
	engine           TEXT NOT NULL DEFAULT '',
	family           TEXT NOT NULL DEFAULT '',
	status           TEXT NOT NULL,
	status_reason    TEXT NOT NULL DEFAULT '',
	record_id        TEXT NOT NULL DEFAULT '',
	uploader_hmac    TEXT NOT NULL DEFAULT '',
	asn_observed     TEXT NOT NULL DEFAULT '',
	country_observed TEXT NOT NULL DEFAULT '',
	asn_hint         TEXT NOT NULL DEFAULT '',
	country_hint     TEXT NOT NULL DEFAULT '',
	created_at       TEXT NOT NULL,
	updated_at       TEXT NOT NULL,
	UNIQUE(set_id, version)
);
CREATE INDEX set_versions_fp ON set_versions(fp, targets_key);
CREATE INDEX set_versions_status ON set_versions(status);

CREATE TABLE votes (
	id               INTEGER PRIMARY KEY AUTOINCREMENT,
	record_id        TEXT NOT NULL DEFAULT '',
	set_id           TEXT NOT NULL,
	version          INTEGER NOT NULL,
	fp               TEXT NOT NULL,
	key_hmac         TEXT NOT NULL,
	kind             TEXT NOT NULL,
	weight           REAL NOT NULL,
	asn_observed     TEXT NOT NULL DEFAULT '',
	country_observed TEXT NOT NULL DEFAULT '',
	asn_hint         TEXT NOT NULL DEFAULT '',
	country_hint     TEXT NOT NULL DEFAULT '',
	origin_verified  INTEGER NOT NULL DEFAULT 0,
	domain           TEXT NOT NULL DEFAULT '',
	b4_version       TEXT NOT NULL DEFAULT '',
	engine           TEXT NOT NULL DEFAULT '',
	bucket           INTEGER NOT NULL,
	received_at      TEXT NOT NULL,
	UNIQUE(key_hmac, fp, asn_observed, bucket)
);
CREATE INDEX votes_fp ON votes(fp);
CREATE INDEX votes_set ON votes(set_id, version);

CREATE TABLE reports (
	id           INTEGER PRIMARY KEY AUTOINCREMENT,
	record_id    TEXT NOT NULL DEFAULT '',
	set_id       TEXT NOT NULL,
	version      INTEGER NOT NULL,
	key_hmac     TEXT NOT NULL,
	asn_observed TEXT NOT NULL DEFAULT '',
	reason       TEXT NOT NULL DEFAULT '',
	received_at  TEXT NOT NULL
);
CREATE INDEX reports_set ON reports(set_id, version);

CREATE TABLE asn_names (
	asn        TEXT PRIMARY KEY,
	name       TEXT NOT NULL,
	country    TEXT NOT NULL DEFAULT '',
	updated_at TEXT NOT NULL
);
`,
	`
CREATE TABLE mirrors (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	url        TEXT NOT NULL UNIQUE,
	key_hmac   TEXT NOT NULL,
	first_seen TEXT NOT NULL,
	last_seen  TEXT NOT NULL,
	status     TEXT NOT NULL DEFAULT 'pending',
	last_check TEXT NOT NULL DEFAULT '',
	last_ok    TEXT NOT NULL DEFAULT '',
	reason     TEXT NOT NULL DEFAULT ''
);
CREATE INDEX mirrors_status ON mirrors(status);
`,
	`
ALTER TABLE keys ADD COLUMN trusted INTEGER NOT NULL DEFAULT 0;
ALTER TABLE keys ADD COLUMN trusted_at TEXT NOT NULL DEFAULT '';
`,
	`
ALTER TABLE set_versions ADD COLUMN original_projection_json TEXT NOT NULL DEFAULT '';
ALTER TABLE set_versions ADD COLUMN edited_at TEXT NOT NULL DEFAULT '';
ALTER TABLE set_versions ADD COLUMN edit_note TEXT NOT NULL DEFAULT '';
`,
	`
ALTER TABLE set_versions ADD COLUMN original_title TEXT NOT NULL DEFAULT '';
ALTER TABLE set_versions ADD COLUMN original_description TEXT NOT NULL DEFAULT '';
`,
	`
ALTER TABLE mirrors ADD COLUMN version TEXT NOT NULL DEFAULT '';
`,
	`
ALTER TABLE set_versions ADD COLUMN hidden_from TEXT NOT NULL DEFAULT '';

ALTER TABLE sets ADD COLUMN withdrawn_at TEXT NOT NULL DEFAULT '';
ALTER TABLE sets ADD COLUMN withdraw_reason TEXT NOT NULL DEFAULT '';

ALTER TABLE reports ADD COLUMN state TEXT NOT NULL DEFAULT 'open';
ALTER TABLE reports ADD COLUMN resolution TEXT NOT NULL DEFAULT '';
ALTER TABLE reports ADD COLUMN note TEXT NOT NULL DEFAULT '';
ALTER TABLE reports ADD COLUMN resolved_at TEXT NOT NULL DEFAULT '';

UPDATE reports SET state = 'dismissed', resolution = 'restored',
	resolved_at = (SELECT v.updated_at FROM set_versions v WHERE v.set_id = reports.set_id AND v.version = reports.version)
	WHERE EXISTS (SELECT 1 FROM set_versions v WHERE v.set_id = reports.set_id AND v.version = reports.version
		AND v.status = 'active' AND reports.received_at <= v.updated_at);
UPDATE reports SET state = 'resolved', resolution = 'rejected',
	resolved_at = (SELECT v.updated_at FROM set_versions v WHERE v.set_id = reports.set_id AND v.version = reports.version)
	WHERE state = 'open' AND EXISTS (SELECT 1 FROM set_versions v WHERE v.set_id = reports.set_id AND v.version = reports.version
		AND v.status = 'rejected');
UPDATE reports SET state = 'resolved', resolution = 'hidden',
	resolved_at = (SELECT v.updated_at FROM set_versions v WHERE v.set_id = reports.set_id AND v.version = reports.version)
	WHERE state = 'open' AND EXISTS (SELECT 1 FROM set_versions v WHERE v.set_id = reports.set_id AND v.version = reports.version
		AND v.status = 'hidden' AND v.status_reason <> 'reports' AND reports.received_at <= v.updated_at);

CREATE INDEX reports_state ON reports(state, received_at);
CREATE INDEX reports_key ON reports(key_hmac);

CREATE TABLE audit_log (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	at          TEXT NOT NULL,
	actor       TEXT NOT NULL,
	actor_ref   TEXT NOT NULL DEFAULT '',
	actor_ip    TEXT NOT NULL DEFAULT '',
	action      TEXT NOT NULL,
	target_kind TEXT NOT NULL,
	target_id   TEXT NOT NULL DEFAULT '',
	version     INTEGER NOT NULL DEFAULT 0,
	reason      TEXT NOT NULL DEFAULT '',
	before_json TEXT NOT NULL DEFAULT '',
	after_json  TEXT NOT NULL DEFAULT '',
	batch_id    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX audit_log_at ON audit_log(at);
CREATE INDEX audit_log_target ON audit_log(target_kind, target_id, id);
CREATE INDEX audit_log_action ON audit_log(action, id);

CREATE TABLE builds (
	id              INTEGER PRIMARY KEY AUTOINCREMENT,
	trigger         TEXT NOT NULL DEFAULT '',
	started_at      TEXT NOT NULL,
	finished_at     TEXT NOT NULL DEFAULT '',
	ok              INTEGER NOT NULL DEFAULT 0,
	error           TEXT NOT NULL DEFAULT '',
	epoch           INTEGER NOT NULL DEFAULT 0,
	seq             INTEGER NOT NULL DEFAULT 0,
	file            TEXT NOT NULL DEFAULT '',
	size            INTEGER NOT NULL DEFAULT 0,
	sets            INTEGER NOT NULL DEFAULT 0,
	blobs           INTEGER NOT NULL DEFAULT 0,
	mirrors         INTEGER NOT NULL DEFAULT 0,
	duration_ms     INTEGER NOT NULL DEFAULT 0,
	content_changed INTEGER NOT NULL DEFAULT 0,
	changes_json    TEXT NOT NULL DEFAULT '{}'
);
CREATE INDEX builds_started ON builds(started_at);

ALTER TABLE mirrors ADD COLUMN check_code TEXT NOT NULL DEFAULT '';
ALTER TABLE mirrors ADD COLUMN check_error TEXT NOT NULL DEFAULT '';
ALTER TABLE mirrors ADD COLUMN check_ms INTEGER NOT NULL DEFAULT 0;
ALTER TABLE mirrors ADD COLUMN served_epoch INTEGER NOT NULL DEFAULT 0;
ALTER TABLE mirrors ADD COLUMN served_seq INTEGER NOT NULL DEFAULT 0;
ALTER TABLE mirrors ADD COLUMN served_generated_at TEXT NOT NULL DEFAULT '';
UPDATE mirrors SET check_error = reason, check_code = 'unknown', reason = '' WHERE status <> 'rejected' AND reason <> '';
`,
	`
ALTER TABLE keys ADD COLUMN name TEXT NOT NULL DEFAULT '';
ALTER TABLE keys ADD COLUMN note TEXT NOT NULL DEFAULT '';
ALTER TABLE keys ADD COLUMN tag TEXT NOT NULL DEFAULT '';
ALTER TABLE keys ADD COLUMN profile_updated_at TEXT NOT NULL DEFAULT '';

CREATE INDEX records_key ON records(key_hmac, received_at);
CREATE INDEX votes_key ON votes(key_hmac);
CREATE INDEX votes_received ON votes(received_at, id);
CREATE INDEX set_versions_uploader ON set_versions(uploader_hmac);
`,
	`
CREATE TABLE reason_presets (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	scope      TEXT NOT NULL,
	label      TEXT NOT NULL DEFAULT '',
	text       TEXT NOT NULL,
	position   INTEGER NOT NULL DEFAULT 0,
	uses       INTEGER NOT NULL DEFAULT 0,
	last_used  TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	UNIQUE(scope, text)
);
CREATE INDEX reason_presets_scope ON reason_presets(scope, position);
`,
	`
CREATE INDEX records_received ON records(received_at, kind);
CREATE INDEX keys_first_seen ON keys(first_seen);
CREATE INDEX reports_received ON reports(received_at, id);
`,
	`
UPDATE set_versions SET hidden_from = 'pending'
WHERE status = 'hidden' AND hidden_from = ''
	AND version > (SELECT current_version FROM sets WHERE sets.id = set_versions.set_id);
`,
}
