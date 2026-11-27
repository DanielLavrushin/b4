import { test } from "node:test";
import assert from "node:assert/strict";
import { withAdoptedHubLink } from "./hubLink";

void test("an imported set keeps which hub set it is and drops the other router's vote", () => {
  assert.deepEqual(
    withAdoptedHubLink({
      name: "youtube",
      hub: {
        id: "yt-9",
        version: 2,
        hash: "abc",
        vote: "works",
        voted_at: "2026-01-01T00:00:00Z",
        applied_at: "2026-01-01T00:00:00Z",
      },
    }),
    { name: "youtube", hub: { id: "yt-9", version: 2, hash: "abc" } },
  );
});

void test("a hub link without an id or a hash is dropped, and a set without one stays as it is", () => {
  assert.deepEqual(
    withAdoptedHubLink({ name: "a", hub: { vote: "broken", voted_at: "x" } }),
    { name: "a" },
  );
  assert.deepEqual(withAdoptedHubLink({ name: "a", hub: "text" }), {
    name: "a",
  });
  assert.deepEqual(withAdoptedHubLink({ name: "a", tcp: { win: {} } }), {
    name: "a",
    tcp: { win: {} },
  });
});
