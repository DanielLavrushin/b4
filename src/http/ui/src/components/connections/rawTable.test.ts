import { test } from "node:test";
import assert from "node:assert/strict";
import { ROW_HEIGHT, visibleRange } from "./rawTable";

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
