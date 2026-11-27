import { test } from "node:test";
import assert from "node:assert/strict";
import { convertLegacySetShape } from "./setImport";

void test("a set exported before 1.29.0 keeps its desync and window settings", () => {
  const old = {
    name: "old",
    tcp: {
      syn_fake: true,
      desync_mode: "rst",
      desync_ttl: 5,
      desync_count: 2,
      post_desync: true,
      win_mode: "oob",
      win_values: [0, 1],
    },
  };
  const before = structuredClone(old);
  assert.deepEqual(convertLegacySetShape(old), {
    name: "old",
    tcp: {
      syn_fake: true,
      desync: { mode: "rst", ttl: 5, count: 2, post_desync: true },
      win: { mode: "oob", values: [0, 1] },
    },
  });
  assert.deepEqual(old, before);
});

void test("only the old fields that are present are carried over", () => {
  assert.deepEqual(
    convertLegacySetShape({ name: "a", tcp: { desync_mode: "rst" } }),
    { name: "a", tcp: { desync: { mode: "rst" } } },
  );
});

void test("a value in today's place wins over the old field", () => {
  assert.deepEqual(
    convertLegacySetShape({
      name: "a",
      tcp: { desync_mode: "rst", desync_ttl: 5, desync: { mode: "ack" } },
    }),
    { name: "a", tcp: { desync: { mode: "ack", ttl: 5 } } },
  );
});

void test("a set in today's format is returned as it is", () => {
  const current = { name: "a", tcp: { desync: { mode: "rst" } } };
  assert.equal(convertLegacySetShape(current), current);
  const bare = { name: "b" };
  assert.equal(convertLegacySetShape(bare), bare);
});
