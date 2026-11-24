import { test } from "node:test";
import assert from "node:assert/strict";
import { MAX_SET_RESOLVERS, resolverList, withResolver } from "./resolvers";

const URL = "https://dns.example/dns-query";

const dns = (over: Record<string, unknown> = {}) => ({
  enabled: true,
  target_dns: "",
  doh_url: "",
  upstreams: undefined as string[] | undefined,
  strict: true,
  ...over,
});

void test("the first resolver of a set is stored by its kind", () => {
  const doh = withResolver(dns(), URL);
  assert.equal(doh.outcome, "added");
  assert.deepEqual(doh.dns, dns({ doh_url: URL }));
  const plain = withResolver(dns(), "192.0.2.5");
  assert.deepEqual(plain.dns, dns({ target_dns: "192.0.2.5" }));
});

void test("a further resolver goes to the end of the list", () => {
  const added = withResolver(
    dns({ target_dns: "192.0.2.5", upstreams: ["https://b.example/dns-query"] }),
    URL,
  );
  assert.equal(added.outcome, "added");
  assert.deepEqual(resolverList(added.dns), [
    "192.0.2.5",
    "https://b.example/dns-query",
    URL,
  ]);
  assert.equal(added.dns.target_dns, "192.0.2.5");
  assert.equal(added.dns.strict, true);
});

void test("a resolver the set already asks is not added twice", () => {
  const set = dns({ doh_url: "https://DNS.example/dns-query" });
  const again = withResolver(set, URL);
  assert.equal(again.outcome, "present");
  assert.equal(again.dns, set);
});

void test("adding a resolver turns the redirect on", () => {
  const off = withResolver(dns({ enabled: false }), URL);
  assert.equal(off.outcome, "added");
  assert.deepEqual(off.dns, dns({ doh_url: URL }));
  const stored = withResolver(dns({ enabled: false, doh_url: URL }), URL);
  assert.equal(stored.outcome, "added");
  assert.deepEqual(stored.dns, dns({ doh_url: URL }));
});

void test("a set whose redirect was off asks the added resolver first", () => {
  const off = { enabled: false, target_dns: "192.0.2.5" };
  const expected = dns({ doh_url: URL, upstreams: ["192.0.2.5"] });
  assert.deepEqual(withResolver(dns(off), URL).dns, expected);
  assert.deepEqual(
    withResolver(dns({ ...off, upstreams: [URL] }), URL).dns,
    expected,
  );
});

void test("a URL kept in the address field is not a resolver the set asks", () => {
  const misplaced = withResolver(dns({ target_dns: URL }), URL);
  assert.equal(misplaced.outcome, "added");
  assert.deepEqual(misplaced.dns, dns({ doh_url: URL }));
  const behind = withResolver(
    dns({ target_dns: URL, upstreams: ["192.0.2.5"] }),
    URL,
  );
  assert.equal(behind.outcome, "added");
  assert.deepEqual(resolverList(behind.dns), ["192.0.2.5", URL]);
});

const MISPLACED = [
  { target_dns: "https://ignored.example/dns-query" },
  { target_dns: " HTTPS://ignored.example/dns-query" },
  { target_dns: "http://ignored.example/dns-query" },
];

void test("a URL kept in the address field is not carried over", () => {
  for (const first of MISPLACED) {
    const set = { ...first, upstreams: ["192.0.2.5"] };
    assert.deepEqual(
      withResolver(dns(set), URL).dns,
      dns({ target_dns: "192.0.2.5", upstreams: [URL] }),
    );
    assert.deepEqual(
      withResolver(dns({ ...set, enabled: false }), URL).dns,
      dns({ doh_url: URL, upstreams: ["192.0.2.5"] }),
    );
    assert.deepEqual(withResolver(dns(first), URL).dns, dns({ doh_url: URL }));
  }
});

void test("a URL kept in the address field takes none of the places", () => {
  const seven = Array.from(
    { length: MAX_SET_RESOLVERS - 1 },
    (_, i) => `https://r${i + 1}.example/dns-query`,
  );
  for (const first of MISPLACED) {
    const set = { ...first, upstreams: seven };
    const added = withResolver(dns(set), URL);
    assert.equal(added.outcome, "added");
    assert.deepEqual(resolverList(added.dns), [...seven, URL]);
    const turnedOn = withResolver(dns({ ...set, enabled: false }), URL);
    assert.equal(turnedOn.outcome, "added");
    assert.deepEqual(resolverList(turnedOn.dns), [URL, ...seven]);
  }
});

void test("a DoH URL stays first whatever the address field holds", () => {
  const own = "https://own.example/dns-query";
  for (const { target_dns } of [...MISPLACED, { target_dns: "192.0.2.9" }]) {
    assert.deepEqual(
      withResolver(dns({ doh_url: own, target_dns }), URL).dns,
      dns({ doh_url: own, upstreams: [URL] }),
    );
  }
});

void test("an entry the user has to correct stays in the list", () => {
  const first = { target_dns: "resolver.local", upstreams: ["192.0.2.5"] };
  assert.deepEqual(resolverList(withResolver(dns(first), URL).dns), [
    "resolver.local",
    "192.0.2.5",
    URL,
  ]);
});

void test("a full list takes no further resolver", () => {
  const full = dns({
    doh_url: "https://r0.example/dns-query",
    upstreams: Array.from(
      { length: MAX_SET_RESOLVERS - 1 },
      (_, i) => `https://r${i + 1}.example/dns-query`,
    ),
  });
  const refused = withResolver(full, URL);
  assert.equal(refused.outcome, "full");
  assert.equal(refused.dns, full);
  assert.equal(
    withResolver(full, "https://r3.example/dns-query").outcome,
    "present",
  );
});

void test("a list stored past the limit with the redirect off stays as it is", () => {
  const stored = dns({
    enabled: false,
    doh_url: "https://r0.example/dns-query",
    upstreams: Array.from(
      { length: MAX_SET_RESOLVERS + 1 },
      (_, i) => `https://r${i + 1}.example/dns-query`,
    ),
  });
  for (const i of [3, 9]) {
    const refused = withResolver(stored, `https://r${i}.example/dns-query`);
    assert.equal(refused.outcome, "full");
    assert.equal(refused.dns, stored);
  }
});
