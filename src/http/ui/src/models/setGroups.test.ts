import { test } from "node:test";
import assert from "node:assert/strict";
import {
  cardDrop,
  groupDrop,
  leanOf,
  nearestSlot,
  parseSetsImport,
  setBlocks,
  setGroupFileName,
} from "./setGroups";
import type { Lean } from "./setGroups";

const list = (spec: string) =>
  spec.split(" ").map((item) => {
    const [id, group = ""] = item.split(":");
    return { id, group };
  });

const shape = (spec: string) =>
  setBlocks(list(spec)).map(
    (block) => `${block.group || "-"}[${block.sets.map((s) => s.id).join(",")}]`,
  );

const spell = (spec: string, ids: string[], moved?: string, group?: string) => {
  const groups = new Map(list(spec).map((set) => [set.id, set.group]));
  if (moved !== undefined) groups.set(moved, group ?? "");
  return ids
    .map((id) => (groups.get(id) ? `${id}:${groups.get(id)}` : id))
    .join(" ");
};

const card = (
  spec: string,
  id: string,
  slot: number,
  lean: Lean,
  visible?: string,
) => {
  const sets = list(spec);
  const shown = visible ? visible.split(" ") : sets.map((set) => set.id);
  const result = cardDrop(sets, shown, id, slot, lean);
  return spell(spec, result.ids, id, result.group);
};

const group = (spec: string, run: string, slot: number, visible?: string) => {
  const sets = list(spec);
  const shown = visible ? visible.split(" ") : sets.map((set) => set.id);
  return spell(spec, groupDrop(sets, shown, run.split(" "), slot));
};

void test("consecutive sets of one group form one block, the rest stand alone", () => {
  assert.deepEqual(shape("a b:yt c:yt d"), ["-[a]", "yt[b,c]", "-[d]"]);
  assert.deepEqual(shape("a:yt b:dc c:yt"), ["yt[a]", "dc[b]", "yt[c]"]);
  assert.deepEqual(shape("a b"), ["-[a]", "-[b]"]);
  assert.deepEqual(setBlocks([]), []);
});

void test("a card picked up and dropped in place changes nothing", () => {
  const spec = "p a:yt b:yt c:yt q one:dc r";
  const order = list(spec).map((set) => set.id);
  for (const id of ["a", "b", "c", "one", "p", "r"]) {
    assert.equal(card(spec, id, order.indexOf(id), 0), spec, id);
  }
});

void test("a card dropped between two sets of a group joins it", () => {
  assert.equal(card("x a:yt b:yt c", "x", 1, 0), "a:yt x:yt b:yt c");
  assert.equal(card("x a:yt b:yt c", "x", 1, -1), "a:yt x:yt b:yt c");
  assert.equal(card("a:yt b:yt c:dc d:dc", "b", 2, 0), "a:yt c:dc b:dc d:dc");
});

void test("next to a group, a card joins only when it leans toward the group", () => {
  assert.equal(card("x a:yt b:yt", "x", 0, 1), "x:yt a:yt b:yt");
  assert.equal(card("x a:yt b:yt", "x", 0, 0), "x a:yt b:yt");
  assert.equal(card("x a:yt b:yt", "x", 0, -1), "x a:yt b:yt");
  assert.equal(card("a:yt b:yt x", "x", 2, -1), "a:yt b:yt x:yt");
  assert.equal(card("a:yt b:yt x", "x", 2, 0), "a:yt b:yt x");
  assert.equal(card("a:yt b:yt c x", "x", 2, -1), "a:yt b:yt x:yt c");
  assert.equal(card("a:yt b:yt c x", "x", 2, 0), "a:yt b:yt x c");
  assert.equal(card("a:yt b:yt c x", "x", 2, 1), "a:yt b:yt x c");
});

void test("a set of a group reorders inside it, up to its first and last place", () => {
  assert.equal(card("p a:yt b:yt c:yt q", "c", 1, 0), "p c:yt a:yt b:yt q");
  assert.equal(card("p a:yt b:yt c:yt q", "c", 1, 1), "p c:yt a:yt b:yt q");
  assert.equal(card("p a:yt b:yt c:yt q", "a", 3, 0), "p b:yt c:yt a:yt q");
  assert.equal(card("p a:yt b:yt c:yt q", "a", 3, -1), "p b:yt c:yt a:yt q");
  assert.equal(card("p a:yt b:yt c:yt q", "a", 2, 1), "p b:yt a:yt c:yt q");
});

void test("a set leaves its group by leaning out at the group's edge", () => {
  assert.equal(card("p a:yt b:yt q", "a", 1, -1), "p a b:yt q");
  assert.equal(card("p a:yt b:yt q", "b", 2, 1), "p a:yt b q");
  assert.equal(card("a:yt b:yt", "b", 1, 1), "a:yt b");
  assert.equal(card("a:yt b:yt", "a", 0, -1), "a b:yt");
  assert.equal(card("a:yt b:yt", "b", 0, -1), "b a:yt");
  assert.equal(card("a:yt b:yt", "a", 1, 1), "b:yt a");
  assert.equal(card("a:yt b:yt c", "b", 2, 0), "a:yt c b");
});

void test("between two groups a card takes the side it leans to, or neither", () => {
  assert.equal(card("a:yt b:dc x", "x", 1, -1), "a:yt x:yt b:dc");
  assert.equal(card("a:yt b:dc x", "x", 1, 0), "a:yt x b:dc");
  assert.equal(card("a:yt b:dc x", "x", 1, 1), "a:yt x:dc b:dc");
  assert.equal(card("a:yt b:yt c:dc d:dc", "b", 1, 1), "a:yt b:dc c:dc d:dc");
  assert.equal(card("a:yt b:yt c:dc d:dc", "b", 1, 0), "a:yt b:yt c:dc d:dc");
});

void test("the only set of a group keeps its group wherever it lands alone", () => {
  assert.equal(card("one:yt a b", "one", 0, -1), "one:yt a b");
  assert.equal(card("one:yt a b", "one", 0, 1), "one:yt a b");
  assert.equal(card("one:yt a b", "one", 2, 0), "a b one:yt");
  assert.equal(card("one:yt a:dc b:dc", "one", 1, 0), "a:dc one:dc b:dc");
});

void test("sets hidden by the search never get split from their group", () => {
  assert.equal(card("a:yt h:yt c x", "x", 1, 0, "a c x"), "a:yt h:yt x c");
  assert.equal(card("a:yt h:yt c x", "x", 1, -1, "a c x"), "a:yt x:yt h:yt c");
  assert.equal(card("c h:yt a:yt x", "x", 1, 0, "c a x"), "c x h:yt a:yt");
  assert.equal(card("c:yt h:yt a:yt", "a", 1, 0, "c a"), "c:yt h:yt a:yt");
  assert.equal(card("a x", "x", 0, 0, "x"), "a x");
});

void test("a dragged group moves whole and never lands inside another group", () => {
  assert.equal(group("a b:yt c:yt d e", "b c", 2), "a d b:yt c:yt e");
  assert.equal(group("a b:yt c:yt d e", "b c", 0), "b:yt c:yt a d e");
  assert.equal(group("a b:yt c:yt d e", "b c", 1), "a b:yt c:yt d e");
  assert.equal(group("a b:yt c:yt d e", "b c", 9), "a d e b:yt c:yt");
  assert.equal(group("a:yt b:dc c:dc d:dc e", "a", 1), "a:yt b:dc c:dc d:dc e");
  assert.equal(group("a:yt b:dc c:dc d:dc e", "a", 2), "b:dc c:dc d:dc a:yt e");
  assert.equal(group("e b:dc c:dc d:dc a:yt", "a", 2), "e a:yt b:dc c:dc d:dc");
  assert.equal(
    group("g:yt h:yt a b:dc k:dc c", "g h", 2, "g a b c"),
    "a b:dc k:dc g:yt h:yt c",
  );
});

void test("the slot is the nearest one and the lean needs a clear offset", () => {
  const slots = [
    { left: 0, top: 0, width: 100, height: 50 },
    { left: 120, top: 0, width: 100, height: 50 },
    { left: 0, top: 70, width: 100, height: 50 },
  ];
  assert.equal(nearestSlot({ x: 50, y: 25 }, slots), 0);
  assert.equal(nearestSlot({ x: 150, y: 30 }, slots), 1);
  assert.equal(nearestSlot({ x: 60, y: 90 }, slots), 2);
  assert.equal(leanOf({ x: 50, y: 25 }, slots[0], false), 0);
  assert.equal(leanOf({ x: 62, y: 0 }, slots[0], false), 0);
  assert.equal(leanOf({ x: 70, y: 25 }, slots[0], false), 1);
  assert.equal(leanOf({ x: 30, y: 25 }, slots[0], false), -1);
  assert.equal(leanOf({ x: 30, y: 25 }, slots[0], true), 0);
  assert.equal(leanOf({ x: 50, y: 40 }, slots[0], true), 1);
  assert.equal(leanOf({ x: 50, y: 10 }, slots[0], true), -1);
});

void test("pasted text is recognised as sets, a group, a shared set or a command line", () => {
  assert.deepEqual(parseSetsImport("  \n "), { kind: "empty" });
  assert.deepEqual(parseSetsImport("nfqws --dpi-desync=fake"), {
    kind: "command",
  });
  assert.deepEqual(parseSetsImport("[Unit]\nExecStart=/opt/nfqws --x"), {
    kind: "command",
  });
  assert.deepEqual(
    parseSetsImport('{"group":" yt ","sets":[{"name":"a"},{"name":"b","tcp":{}}]}'),
    { kind: "sets", group: "yt", sets: [{ name: "a" }, { name: "b", tcp: {} }] },
  );
  assert.deepEqual(parseSetsImport('{"name":"a","targets":{}}'), {
    kind: "sets",
    group: "",
    sets: [{ name: "a", targets: {} }],
  });
  assert.deepEqual(parseSetsImport('[{"name":"a"},{"name":"b"}]'), {
    kind: "sets",
    group: "",
    sets: [{ name: "a" }, { name: "b" }],
  });
  assert.deepEqual(
    parseSetsImport('{"name":"a } \\" {"}\n\n{"name":"b"}, {"name":"c","udp":{}}'),
    {
      kind: "sets",
      group: "",
      sets: [{ name: 'a } " {' }, { name: "b" }, { name: "c", udp: {} }],
    },
  );
  const envelope = { format: 1, set: { name: "a" }, fingerprint: "x" };
  assert.deepEqual(parseSetsImport(JSON.stringify(envelope)), {
    kind: "envelope",
    envelope,
  });
  assert.deepEqual(parseSetsImport('{"name":"a"'), {
    kind: "invalid",
    reason: "json",
  });
  assert.deepEqual(parseSetsImport('{"version":52,"sets":[{"name":"a"}]}'), {
    kind: "invalid",
    reason: "config",
  });
  for (const text of [
    "[]",
    '{"group":"yt","sets":[]}',
    '{"sets":[{"name":"a"},"b"]}',
    '{"hello":1}',
    '{"name":"a"} {"hello":1}',
    '[{"name":"a"},{"targets":{"sni_domains":["a.example.org"]}}]',
    '{"name":"  ","tcp":{}}',
    '{"name":7,"tcp":{}}',
    "42",
  ]) {
    assert.deepEqual(
      parseSetsImport(text),
      { kind: "invalid", reason: "unknown" },
      text,
    );
  }
});

void test("the export file name keeps letters of any script", () => {
  assert.equal(setGroupFileName("YouTube apps"), "youtube-apps.b4group.json");
  assert.equal(setGroupFileName("Ютуб"), "ютуб.b4group.json");
  assert.equal(setGroupFileName("  --  "), "group.b4group.json");
});
