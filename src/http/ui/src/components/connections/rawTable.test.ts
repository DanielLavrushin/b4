import { test } from "node:test";
import assert from "node:assert/strict";
import type { ParsedLog } from "../../hooks/useDomainActions";
import type { AsnLabels } from "../../utils/asn";
import {
  COLUMN_WIDTHS,
  ROW_HEIGHT,
  TABLE_MIN_WIDTH,
  sameRow,
  visibleRange,
} from "./rawTable";

const DRAWER_AND_PADDING = 240 + 50;
const GOOGLEVIDEO_WITH_BADGE = 335;
const IPV4_DESTINATION_WITH_ICONS = 231;
const DOMAIN = 3;
const DESTINATION = 5;

const columnWidths = (table: number): number[] => {
  const set = COLUMN_WIDTHS.map((width) => {
    if (typeof width === "number") return width;
    if (typeof width === "string") return (parseFloat(width) / 100) * table;
    return 0;
  });
  const rest = table - set.reduce((sum, width) => sum + width, 0);
  return COLUMN_WIDTHS.map((width, i) => (width === undefined ? rest : set[i]));
};

void test("the raw feed fits beside the drawer on a 1366 px laptop", () => {
  assert.ok(TABLE_MIN_WIDTH.lg <= 1366 - DRAWER_AND_PADDING);
});

void test("on a tablet or phone the domain column keeps room for a googlevideo name", () => {
  assert.ok(columnWidths(TABLE_MIN_WIDTH.xs)[DOMAIN] >= GOOGLEVIDEO_WITH_BADGE);
});

void test("on a 1440 px laptop the domain column keeps room for a googlevideo name", () => {
  assert.ok(columnWidths(1440 - DRAWER_AND_PADDING)[DOMAIN] >= GOOGLEVIDEO_WITH_BADGE);
});

void test("on a 1366 px laptop the destination column keeps room for an IPv4 address and its icons", () => {
  assert.ok(columnWidths(1366 - DRAWER_AND_PADDING)[DESTINATION] >= IPV4_DESTINATION_WITH_ICONS);
});

const viewport = 444;
const bottomOf = (count: number) => count * ROW_HEIGHT + 38 - viewport;

void test("a list that shrinks under a view scrolled far down still shows its rows", () => {
  const { start, end } = visibleRange(bottomOf(1000), viewport, 5);
  assert.equal(start, 0);
  assert.equal(end, 5);
});

void test("at the bottom of a long list the rows cover the whole view", () => {
  const top = bottomOf(1000);
  const { start, end } = visibleRange(top, viewport, 1000);
  assert.equal(end, 1000);
  assert.ok(start * ROW_HEIGHT <= top);
  assert.ok(end * ROW_HEIGHT >= top + viewport - 38);
});

void test("the window follows the scroll position inside the list", () => {
  assert.deepEqual(visibleRange(0, viewport, 1000), { start: 0, end: 21 });
  assert.deepEqual(visibleRange(41 * 100, viewport, 1000), { start: 95, end: 116 });
});

const log: ParsedLog = {
  timestamp: "2026/11/28 15:31:21",
  protocol: "TCP",
  hostSet: "",
  ipSet: "",
  domain: "redirector.googlevideo.com",
  source: "192.168.1.23:51234",
  destination: "85.249.244.49:443",
  raw: "line",
  sourceAlias: "aa-bb-cc-dd-ee-ff",
  deviceName: "",
  tls: "1.3",
  flags: "",
};
const enrichingIps = new Set<string>();
const asnLabels = {} as AsnLabels;

void test("a row on screen is drawn again when its device name arrives", () => {
  const before = { log, enrichingIps, asnLabels };
  const after = { log: { ...log, deviceName: "iPad" }, enrichingIps, asnLabels };
  assert.equal(sameRow(before, after), false);
});

void test("a row rebuilt with the same contents is not drawn again", () => {
  const before = { log: { ...log, deviceName: "iPad" }, enrichingIps, asnLabels };
  const after = { log: { ...log, deviceName: "iPad" }, enrichingIps, asnLabels };
  assert.equal(sameRow(before, after), true);
});
