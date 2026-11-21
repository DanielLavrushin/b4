import { test } from "node:test";
import assert from "node:assert/strict";
import type { B4SetConfig, TargetsConfig } from "../models/config";
import { dscpRefusal, dscpRefusalKey, keepLocalDscp } from "./dscp";

const dscpSet = (over: Partial<TargetsConfig> = {}): B4SetConfig =>
  ({
    id: "set",
    name: "dscp",
    enabled: true,
    targets: {
      sni_domains: [],
      ip: [],
      geosite_categories: [],
      geoip_categories: [],
      asns: [],
      ...over,
    },
    routing: { enabled: false },
    dscp: { enabled: true, value: 7 },
  }) as unknown as B4SetConfig;

const cases: [string, Partial<TargetsConfig>, string | null][] = [
  ["no targets", {}, "no_addresses"],
  ["Use collected IPs alone", { use_collected_ips: true }, null],
  ["domain-only with Use collected IPs", { domain_only: true, use_collected_ips: true }, null],
  ["domain-only with domains alone", { domain_only: true, sni_domains: ["example.com"] }, "no_addresses"],
  ["domain-only with an address", { domain_only: true, ip: ["203.0.113.0/24"] }, null],
  ["a domain", { sni_domains: ["example.com"] }, null],
  ["Use collected IPs off", { use_collected_ips: false }, "no_addresses"],
];

void test("dscpRefusal counts Use collected IPs as an address target, as Go DSCPRefusal does", () => {
  for (const [name, targets, want] of cases) {
    assert.equal(dscpRefusal(dscpSet(targets), false), want, name);
  }
});

void test("a domain-only set without address targets names domain-only in its refusal", () => {
  const set = dscpSet({ domain_only: true, sni_domains: ["example.com"] });
  assert.equal(dscpRefusalKey(set, "no_addresses"), "sets.routing.dscpDomainOnly");
  assert.equal(dscpRefusalKey(dscpSet(), "no_addresses"), "sets.routing.dscpNoAddresses");
});

void test("a hub import keeps DSCP on in a local set whose only target is Use collected IPs", () => {
  const local = dscpSet({ use_collected_ips: true });
  assert.deepEqual(keepLocalDscp(local, false), { dscp: local.dscp, warnings: [] });
  const empty = dscpSet();
  assert.deepEqual(keepLocalDscp(empty, false), {
    dscp: { enabled: false, value: 7 },
    warnings: [{ code: "dscp_off", params: { value: 7 } }],
  });
});
