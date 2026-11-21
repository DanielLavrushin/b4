import { test } from "node:test";
import assert from "node:assert/strict";
import { firstByteWait } from "./firstByte";

const goDelays = (low: number, high: number): number[] => {
  const drawn: number[] = [];
  const top = high <= low ? low : high;
  for (let ms = low; ms <= top; ms++) drawn.push(ms < 10 ? 30 : ms);
  return drawn;
};

const label = (low: number, high: number) => firstByteWait(low, high, " ms");

void test("a single delay shows the wait b4 uses, with the jitter mark", () => {
  assert.equal(label(0, 0), "30 ms+");
  assert.equal(label(5, 0), "30 ms+");
  assert.equal(label(10, 10), "10 ms+");
  assert.equal(label(50, 20), "50 ms+");
});

void test("a range below 10 ms is the 30 ms fallback", () => {
  assert.equal(label(0, 5), "30 ms+");
});

void test("a range that crosses 10 ms shows the fallback as a possible wait", () => {
  assert.equal(label(0, 10), "10-30 ms+");
  assert.equal(label(0, 20), "10-30 ms+");
  assert.equal(label(0, 100), "10-100 ms+");
});

void test("a range from 10 ms up keeps its bounds and the jitter mark", () => {
  assert.equal(label(20, 50), "20-50 ms+");
  assert.equal(label(10, 1000), "10-1000 ms+");
});

void test("the label spans exactly the delays the Go sender can draw", () => {
  for (const [low, high] of [
    [0, 0],
    [0, 10],
    [0, 20],
    [0, 100],
    [10, 10],
    [20, 50],
    [5, 9],
  ]) {
    const drawn = goDelays(low, high);
    const from = Math.min(...drawn);
    const to = Math.max(...drawn);
    const want = from === to ? `${from} ms+` : `${from}-${to} ms+`;
    assert.equal(label(low, high), want, `${low}-${high}`);
  }
});
