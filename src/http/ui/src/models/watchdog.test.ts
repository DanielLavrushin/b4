import { test } from "node:test";
import assert from "node:assert/strict";
import { anySetWatched, setWatchBlock } from "./watchdog";

type Watchable = Parameters<typeof setWatchBlock>[0];

const flagged = (change: (set: Watchable) => void = () => {}): Watchable => {
  const set: Watchable = {
    enabled: true,
    routing: { enabled: false },
    discovery: { urls: ["https://example.com/"], watchdog: true },
    targets: { sni_domains: ["example.com"], geosite_categories: [] },
  };
  change(set);
  return set;
};

void test("a flagged set the watchdog can watch counts as watched", () => {
  assert.equal(anySetWatched([flagged()]), true);
  assert.equal(
    anySetWatched([flagged((s) => (s.enabled = false)), flagged()]),
    true,
  );
});

void test("no sets, or no flagged set, means nothing is watched", () => {
  assert.equal(anySetWatched(undefined), false);
  assert.equal(anySetWatched([]), false);
  assert.equal(
    anySetWatched([flagged((s) => (s.discovery = { urls: ["https://example.com/"] }))]),
    false,
  );
});

void test("a flagged set the watchdog cannot watch does not count", () => {
  const unwatchable: Record<string, (set: Watchable) => void> = {
    set_disabled: (s) => (s.enabled = false),
    no_urls: (s) => (s.discovery = { urls: [], watchdog: true }),
    ip_only: (s) => (s.targets = { sni_domains: [], geosite_categories: [] }),
    routed_set: (s) => (s.routing = { enabled: true }),
    device_scoped: (s) =>
      (s.targets = { ...s.targets, source_devices: ["aa:bb:cc:dd:ee:ff"] }),
  };
  for (const [block, change] of Object.entries(unwatchable)) {
    const set = flagged(change);
    assert.equal(setWatchBlock(set), block);
    assert.equal(anySetWatched([set]), false, block);
  }
});
