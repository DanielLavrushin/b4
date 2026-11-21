import { test } from "node:test";
import assert from "node:assert/strict";
import { normalizeFrame } from "./metricsMerge";

const now = 1_800_000_000_000;

void test("a frame keeps when each collected address expires and how often a full pool turned one away", () => {
  const frame = normalizeFrame({
    type: "tick",
    now,
    collected: {
      rev: 4,
      refused: 7,
      items: [
        { ip: "203.0.113.9", host: "video.example", set: "Video", since: now - 60_000, until: now + 21_540_000 },
        { ip: "198.51.100.4", host: "", set: "Video", since: now - 120_000 },
      ],
    },
  });
  assert.ok(frame?.collected);
  assert.equal(frame.collected.rev, 4);
  assert.equal(frame.collected.refused, 7);
  assert.deepEqual(
    frame.collected.items.map((entry) => entry.until),
    [now + 21_540_000, 0],
  );
});

void test("a collected list without a refusal count reads as none refused", () => {
  const frame = normalizeFrame({ type: "tick", now, collected: { rev: 1, items: [] } });
  assert.equal(frame?.collected?.refused, 0);
});
