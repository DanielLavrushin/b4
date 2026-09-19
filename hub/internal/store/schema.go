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
}
