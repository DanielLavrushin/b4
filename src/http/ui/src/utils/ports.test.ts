import { test } from "node:test";
import assert from "node:assert/strict";
import { portFilterCovers } from "./ports";

void test("an empty port filter covers every port", () => {
  assert.equal(portFilterCovers("", 443), true);
  assert.equal(portFilterCovers(" , ", 8443), true);
});

void test("a port filter covers the ports and ranges it lists", () => {
  assert.equal(portFilterCovers("443", 443), true);
  assert.equal(portFilterCovers("80, 443", 443), true);
  assert.equal(portFilterCovers("400-500", 443), true);
  assert.equal(portFilterCovers("400:500", 443), true);
  assert.equal(portFilterCovers("8000-9000", 8443), true);
});

void test("a port filter leaves out the ports it does not list", () => {
  assert.equal(portFilterCovers("8443", 443), false);
  assert.equal(portFilterCovers("80,5222,8000-9000", 443), false);
  assert.equal(portFilterCovers("444-500", 443), false);
  assert.equal(portFilterCovers("443", 8443), false);
});

const goCovers443: [string, boolean][] = [
  ["", true],
  ["443", true],
  ["8443", false],
  ["80,443", true],
  ["400-500", true],
  ["400:500", true],
  ["x-y", true],
  ["abc", true],
  ["500-400", true],
  ["80,443-443", false],
  ["443-443", true],
  ["443:443", true],
  ["80,400-70000", false],
  ["70000", true],
  ["65536", true],
  ["0", true],
  ["443x", true],
  ["1-2-3", true],
  ["0x1bb", true],
  ["8443,0x1bb", false],
  [" , ", true],
  ["80, 443 ", true],
  ["4e2", true],
  ["443.0", true],
  ["+443", true],
  ["0443", true],
  ["443 - 500", true],
  ["-443", true],
  ["443-", true],
  [",443", true],
  ["1-65535", true],
  ["65535", false],
  ["8443,abc", false],
  ["80,x-y", false],
];

void test("portFilterCovers reads a filter the way b4 saves it: invalid entries are dropped, and a filter left empty covers every port", () => {
  for (const [filter, want] of goCovers443) {
    assert.equal(portFilterCovers(filter, 443), want, JSON.stringify(filter));
  }
});
