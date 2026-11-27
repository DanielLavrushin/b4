import { test } from "node:test";
import assert from "node:assert/strict";
import type { B4SetConfig } from "./config";
import { duplicateSetDraft } from "./sets";

void test("a duplicated set keeps its hub origin but not the hub group stamp", () => {
  const set = {
    id: "a",
    revision: "r1",
    name: "Video A",
    group: "video",
    targets: { sni_domains: ["a.example"], ip_priority: true },
    discovery: { urls: [], watchdog: true },
    hub: {
      id: "s1",
      version: 2,
      hash: "h",
      group_id: "g-video",
      group_version: 3,
    },
  } as unknown as B4SetConfig;

  const copy = duplicateSetDraft(set) as B4SetConfig;

  assert.equal(copy.id, undefined);
  assert.equal(copy.revision, undefined);
  assert.equal(copy.name, "Video A (copy)");
  assert.equal(copy.group, "video");
  assert.deepEqual(copy.hub, { id: "s1", version: 2, hash: "h" });
  assert.equal(copy.discovery?.watchdog, false);
  assert.equal(copy.targets.ip_priority, false);
  assert.equal(set.hub?.group_id, "g-video");
});
