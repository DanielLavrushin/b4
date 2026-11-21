import { test } from "node:test";
import assert from "node:assert/strict";
import type { B4SetConfig } from "../models/config";
import { domainOnlyRoutesNothing } from "./setNotices";

const baseSet = (): B4SetConfig =>
  ({
    id: "set",
    name: "notices",
    enabled: true,
    targets: {
      sni_domains: ["example.com"],
      ip: [],
      geosite_categories: [],
      geoip_categories: [],
      asns: [],
      domain_only: false,
      use_collected_ips: false,
    },
    tcp: {
      dport_filter: "",
      wssize: { enabled: false, window: 0, scale: 0 },
      wssize_reuse: { enabled: false, max_age_seconds: 300 },
      duplicate: { enabled: false, count: 3 },
      ip_block_detect: { enabled: false, action: "block", cache_blocked_ips: true },
    },
    routing: { enabled: false, mode: "interface", egress_interface: "" },
    escalate: {},
  }) as unknown as B4SetConfig;

const shaped = (change: (set: B4SetConfig) => void): B4SetConfig => {
  const set = baseSet();
  change(set);
  return set;
};

void test("a routed domain-only set with domains routes nothing", () => {
  for (const mode of ["interface", "proxy", "mtproto-ws"] as const) {
    assert.equal(
      domainOnlyRoutesNothing(
        shaped((s) => {
          s.targets.domain_only = true;
          s.routing.enabled = true;
          s.routing.mode = mode;
        }),
      ),
      true,
      mode,
    );
  }
});

void test("a domain-only block set blocks by name, so it gets no routing warning", () => {
  assert.equal(
    domainOnlyRoutesNothing(
      shaped((s) => {
        s.targets.domain_only = true;
        s.routing.enabled = true;
        s.routing.mode = "block";
      }),
    ),
    false,
  );
});

void test("no routing warning without domain-only, routing or domains", () => {
  assert.equal(
    domainOnlyRoutesNothing(shaped((s) => (s.routing.enabled = true))),
    false,
  );
  assert.equal(
    domainOnlyRoutesNothing(shaped((s) => (s.targets.domain_only = true))),
    false,
  );
  assert.equal(
    domainOnlyRoutesNothing(
      shaped((s) => {
        s.targets.domain_only = true;
        s.routing.enabled = true;
        s.targets.sni_domains = [];
      }),
    ),
    false,
  );
});
