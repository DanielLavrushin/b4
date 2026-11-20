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
});

void test("a port filter leaves out the ports it does not list", () => {
  assert.equal(portFilterCovers("8443", 443), false);
  assert.equal(portFilterCovers("80,5222,8000-9000", 443), false);
  assert.equal(portFilterCovers("444-500", 443), false);
  assert.equal(portFilterCovers("x-y", 443), false);
});
