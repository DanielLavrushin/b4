import { test } from "node:test";
import assert from "node:assert/strict";
import { compactJson } from "./clipboard";

void test("copied JSON is one line with the same content", () => {
  const shown = JSON.stringify(
    { b4_version: "dev", group: "yt", sets: [{ name: "a b", tcp: { x: 1 } }] },
    null,
    2,
  );
  const copied = compactJson(shown);
  assert.equal(copied.includes("\n"), false);
  assert.deepEqual(JSON.parse(copied), JSON.parse(shown));
  assert.equal(
    copied,
    '{"b4_version":"dev","group":"yt","sets":[{"name":"a b","tcp":{"x":1}}]}',
  );
});

void test("text that is not JSON is copied as it is", () => {
  assert.equal(compactJson("{ broken"), "{ broken");
});
