import { test } from "node:test";
import assert from "node:assert/strict";
import type {
  HubGroup,
  HubGroupApplyResponse,
  HubGroupAppliedSet,
  HubGroupMember,
  HubGroupState,
  HubSet,
} from "./hub";
import {
  hubGroupAction,
  hubGroupCovered,
  hubGroupDescriptionOf,
  hubGroupEscalationTarget,
  hubGroupMemberOf,
  hubGroupMoved,
  hubGroupTakenOver,
  hubGroupUndo,
  hubGroupWarnings,
} from "./hubGroups";

const hubGroupIn = (state: HubGroupState): HubGroup => ({
  id: "g-1",
  version: 2,
  title: "video",
  author: "a",
  created_at: "",
  updated_at: "",
  members: [],
  display: { bucket: "none", score: 0, n: 0, devices: 0 },
  applied: { state, set_ids: [] },
});

const member = (
  name: string,
  extra: Partial<HubGroupAppliedSet> = {},
): HubGroupAppliedSet => ({
  id: `local-${name}`,
  name,
  hub_id: `hub-${name}`,
  version: 1,
  created: true,
  moved: [],
  covered: [],
  warnings: [],
  payloads: [],
  ...extra,
});

const answer = (sets: HubGroupAppliedSet[]): HubGroupApplyResponse => ({
  group: "video",
  sets,
  created: sets.map((s) => s.id),
  ungrouped: [],
});

void test("the group's applied state picks one action, and an applied group offers no second copy", () => {
  assert.equal(hubGroupAction(hubGroupIn("none")), "apply");
  assert.equal(hubGroupAction(hubGroupIn("applied")), "applied");
  assert.equal(hubGroupAction(hubGroupIn("update")), "update");
  assert.equal(hubGroupAction(hubGroupIn("modified")), "reapply");
});

void test("a local set covered by two members is listed once, idle when either member covers all of it", () => {
  const covered = hubGroupCovered(
    answer([
      member("tcp", {
        covered: [
          { set_id: "old", set_name: "Old", geosite: ["youtube"], geoip: [], asns: [], idle: false },
        ],
      }),
      member("quic", {
        covered: [
          { set_id: "old", set_name: "Old", geosite: ["google"], geoip: [], asns: ["15169"], idle: true },
          { set_id: "other", set_name: "Other", geosite: [], geoip: [], asns: [], idle: false },
        ],
      }),
    ]),
  );
  assert.deepEqual(covered, [
    { set_id: "old", set_name: "Old", geosite: ["youtube", "google"], geoip: [], asns: ["15169"], idle: true },
    { set_id: "other", set_name: "Other", geosite: [], geoip: [], asns: [], idle: false },
  ]);
});

void test("moved domains and warnings of every member are reported, each warning with its member's name", () => {
  const res = answer([
    member("tcp", {
      moved: [{ domain: "a.example", set_name: "Old", set_id: "old" }],
      warnings: [{ code: "pins" }],
    }),
    member("quic", {
      moved: [{ domain: "b.example", set_name: "Old", set_id: "old" }],
    }),
  ]);
  assert.deepEqual(
    hubGroupMoved(res).map((m) => m.domain),
    ["a.example", "b.example"],
  );
  assert.deepEqual(hubGroupWarnings(res), [
    { name: "tcp", warning: { code: "pins" } },
  ]);
});

void test("a refusal names its member even at position 0, and an error without a member names none", () => {
  assert.deepEqual(
    hubGroupMemberOf({ code: "invalid_set", member: 0, set_id: "s1", set_name: "YouTube" }),
    { member: 0, name: "YouTube", setId: "s1" },
  );
  assert.deepEqual(
    hubGroupMemberOf({ code: "payload_missing", member: 2, hub_id: "h", set_name: "QUIC" }),
    { member: 2, name: "QUIC", setId: "" },
  );
  assert.equal(hubGroupMemberOf({ code: "hub_no_groups" }), null);
  assert.equal(hubGroupMemberOf("text"), null);
  assert.equal(hubGroupMemberOf(undefined), null);
});

void test("the publish description starts from the hub group this local group is the copy of", () => {
  const listed = (id: string, group?: string, description?: string): HubGroup => ({
    ...hubGroupIn(group ? "applied" : "none"),
    id,
    description,
    applied: { state: group ? "applied" : "none", group, set_ids: [] },
  });
  const groups = [
    listed("g-other", undefined, "not applied here"),
    listed("g-video", "video", "Opens the video player"),
    listed("g-music", "music"),
  ];
  assert.equal(hubGroupDescriptionOf(groups, "video"), "Opens the video player");
  assert.equal(hubGroupDescriptionOf(groups, "music"), "");
  assert.equal(hubGroupDescriptionOf(groups, "games"), "");
  assert.equal(hubGroupDescriptionOf(undefined, "video"), "");
});

void test("the sets a group apply takes over are named before it runs", () => {
  const listed = (title: string, extra: Partial<HubGroupMember> = {}): HubGroupMember => ({
    set: { id: `hub-${title}`, title } as HubSet,
    ...extra,
  });
  const hubGroup: HubGroup = {
    ...hubGroupIn("none"),
    members: [
      listed("a"),
      listed("c", { replaces: { set_id: "local-c", set_name: "My C" } }),
      listed("d", { replaces: { set_id: "local-d", set_name: "D" } }),
    ],
  };
  assert.deepEqual(hubGroupTakenOver(hubGroup), ["My C", "D"]);
  assert.deepEqual(hubGroupTakenOver(hubGroupIn("none")), []);
});

void test("only a first apply that created every set can be undone", () => {
  const created = answer([member("a"), member("c")]);
  assert.deepEqual(hubGroupUndo(created, false), ["local-a", "local-c"]);
  assert.deepEqual(hubGroupUndo({ ...created, created: ["local-a"] }, false), []);
  assert.deepEqual(hubGroupUndo(created, true), []);
});

void test("a member another member escalates to is known by its position", () => {
  const hubGroup: HubGroup = {
    ...hubGroupIn("applied"),
    members: [
      { set: { id: "hub-a" } as HubSet, escalate: { to: 1 } },
      { set: { id: "hub-b" } as HubSet },
      { set: { id: "hub-c" } as HubSet },
    ],
  };
  assert.equal(hubGroupEscalationTarget(hubGroup, 0), false);
  assert.equal(hubGroupEscalationTarget(hubGroup, 1), true);
  assert.equal(hubGroupEscalationTarget(hubGroup, 2), false);
});
