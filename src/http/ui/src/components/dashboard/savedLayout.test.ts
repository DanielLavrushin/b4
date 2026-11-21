import { test } from "node:test";
import assert from "node:assert/strict";
import { joinRows, splitRows, wrapRows } from "./layoutRows";
import { savedLayouts, type PanelSlot } from "./savedLayout";

const panels: PanelSlot[] = [
  { id: "activity", defaultSpan: 12 },
  { id: "sets", defaultSpan: 8 },
  { id: "events", defaultSpan: 4 },
  { id: "domains", defaultSpan: 6 },
  { id: "addresses", defaultSpan: 6 },
  { id: "devices", defaultSpan: 12 },
  { id: "escalations", defaultSpan: 6 },
  { id: "blocked", defaultSpan: 6 },
  { id: "bans", defaultSpan: 6 },
  { id: "collected", defaultSpan: 6 },
  { id: "telegram", defaultSpan: 12 },
];

const { defaultSpanOf, emptyLayout, isCustomized, normalize } = savedLayouts(panels);

const cachedBeforeCollected = {
  v: 3,
  order: [
    "activity",
    "sets",
    "events",
    "domains",
    "addresses",
    "devices",
    "escalations",
    "blocked",
    "bans",
    "telegram",
  ],
  breaks: ["sets", "domains", "devices", "escalations", "bans", "telegram"],
  hidden: [] as string[],
  spans: {} as Record<string, number>,
};

const defaultWithout = (...missing: string[]) =>
  joinRows(
    wrapRows(
      panels.map((panel) => panel.id).filter((id) => !missing.includes(id)),
      defaultSpanOf,
    ),
  );

const rowsOf = (layout: { order: string[]; breaks: string[] }) =>
  splitRows(layout.order, layout.breaks);

void test("the cached layout is the default the dashboard wrote before the collected panel existed", () => {
  const { order, breaks } = defaultWithout("collected");
  assert.deepEqual(order, cachedBeforeCollected.order);
  assert.deepEqual(breaks, cachedBeforeCollected.breaks);
  assert.deepEqual(rowsOf(emptyLayout()).slice(-2), [["bans", "collected"], ["telegram"]]);
});

void test("a dashboard nobody arranged before the collected panel existed is not customized", () => {
  const layout = normalize(cachedBeforeCollected);
  assert.equal(
    isCustomized(layout),
    false,
    `the default cached before the collected panel existed offers Reset layout, rows ${JSON.stringify(rowsOf(layout))}`,
  );
  assert.deepEqual(layout, emptyLayout());
});

void test("a default cached before two panels existed takes the current default", () => {
  const layout = normalize({ ...defaultWithout("collected", "telegram"), hidden: [], spans: {} });
  assert.equal(
    isCustomized(layout),
    false,
    `the default cached before the collected and Telegram panels existed offers Reset layout, rows ${JSON.stringify(rowsOf(layout))}`,
  );
  assert.deepEqual(layout, emptyLayout());
});

void test("an arranged dashboard keeps its rows and gets the collected panel on a row of its own", () => {
  const layout = normalize({
    order: ["telegram", ...cachedBeforeCollected.order.filter((id) => id !== "telegram")],
    breaks: ["activity", ...cachedBeforeCollected.breaks.filter((id) => id !== "telegram")],
  });
  assert.equal(isCustomized(layout), true);
  assert.deepEqual(rowsOf(layout), [
    ["telegram"],
    ["activity"],
    ["sets", "events"],
    ["domains", "addresses"],
    ["devices"],
    ["escalations", "blocked"],
    ["bans"],
    ["collected"],
  ]);
});

void test("hidden panels and widths saved with the old default are kept and still count as customized", () => {
  const layout = normalize({ ...cachedBeforeCollected, hidden: ["events"], spans: { sets: 7 } });
  const defaults = emptyLayout();
  assert.deepEqual(layout.order, defaults.order);
  assert.deepEqual(layout.breaks, defaults.breaks);
  assert.deepEqual(layout.hidden, ["events"]);
  assert.deepEqual(layout.spans, { sets: 7 });
  assert.equal(isCustomized(layout), true);
});
