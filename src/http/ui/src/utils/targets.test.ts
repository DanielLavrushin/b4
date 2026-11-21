import { test } from "node:test";
import assert from "node:assert/strict";
import type { TargetsConfig } from "../models/config";
import { declaresIpTargets, mssClampIpScope } from "./targets";

const targets = (over: Partial<TargetsConfig> = {}): TargetsConfig => ({
  sni_domains: [],
  ip: [],
  geosite_categories: [],
  geoip_categories: [],
  asns: [],
  ...over,
});

const ipCases: [string, TargetsConfig, boolean][] = [
  ["no targets", targets(), false],
  ["Use collected IPs alone", targets({ use_collected_ips: true }), true],
  ["an address", targets({ ip: ["203.0.113.0/24"] }), true],
  ["a GeoIP category", targets({ geoip_categories: ["ru"] }), true],
  ["an ASN", targets({ asns: ["13335"] }), true],
  ["a domain", targets({ sni_domains: ["example.com"] }), false],
  ["a GeoSite category", targets({ geosite_categories: ["youtube"] }), false],
  ["a source device", targets({ source_devices: ["aa:bb:cc:dd:ee:ff"] }), false],
  ["Use collected IPs off", targets({ use_collected_ips: false }), false],
];

void test("declaresIpTargets counts Use collected IPs as an IP target, as Go DeclaresIPTargets does", () => {
  for (const [name, set, want] of ipCases) {
    assert.equal(declaresIpTargets(set), want, name);
  }
});

void test("mssClampIpScope counts Use collected IPs as an MSS clamp scope, as Go validation does", () => {
  for (const [name, set, want] of ipCases) {
    assert.equal(mssClampIpScope(set), want, name);
  }
});

void test("missing target lists count as empty", () => {
  assert.equal(declaresIpTargets(undefined), false);
  assert.equal(declaresIpTargets({}), false);
  assert.equal(mssClampIpScope({ use_collected_ips: true }), true);
});
