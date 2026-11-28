import { test } from "node:test";
import assert from "node:assert/strict";
import type { ParsedLog } from "../../hooks/useDomainActions";
import type { AsnLabels } from "../../utils/asn";
import { ROW_HEIGHT, sameRow, visibleRange } from "./rawTable";

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
