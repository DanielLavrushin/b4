import { test } from "node:test";
import assert from "node:assert/strict";
import type { B4SetConfig } from "../models/config";
import {
  isWSSizeReuseRefusal,
  wssizeReuseIssues,
  type WSSizeReuseEnv,
  type WSSizeReuseIssue,
} from "./wssizeReuse";

const reuseSet = (): B4SetConfig =>
  ({
    id: "set",
    name: "reuse",
    enabled: true,
    targets: {
      sni_domains: [],
      ip: ["203.0.113.0/24"],
      geosite_categories: [],
      geoip_categories: [],
      asns: [],
    },
    tcp: {
      dport_filter: "",
      wssize: { enabled: true, window: 1, scale: 0 },
      wssize_reuse: { enabled: true, max_age_seconds: 120 },
      duplicate: { enabled: false, count: 3 },
    },
    routing: { enabled: false },
    escalate: {},
  }) as unknown as B4SetConfig;

const nfqueue: WSSizeReuseEnv = { tunEngine: false, skipSetup: false, queueMark: 0 };

const issues = (
  change: (set: B4SetConfig) => void,
  env: Partial<WSSizeReuseEnv> = {},
): WSSizeReuseIssue[] => {
  const set = reuseSet();
  change(set);
  return wssizeReuseIssues(set, { ...nfqueue, ...env });
};

void test("a reuse set Go accepts has no issue", () => {
  assert.deepEqual(issues(() => {}), []);
  assert.deepEqual(
    issues((s) => {
      s.targets.ip = [];
      s.targets.use_collected_ips = true;
    }),
    [],
  );
  assert.deepEqual(issues((s) => (s.tcp.dport_filter = "80,443")), []);
  assert.deepEqual(issues((s) => (s.tcp.dport_filter = "400-500")), []);
});

void test("the port refusal follows the filter b4 saves, not the text typed", () => {
  for (const filter of ["80,443-443", "80,400-70000", "8443,0x1bb"]) {
    assert.deepEqual(issues((s) => (s.tcp.dport_filter = filter)), ["port"], filter);
  }
  for (const filter of ["x-y", "500-400", "70000", "443-443"]) {
    assert.deepEqual(issues((s) => (s.tcp.dport_filter = filter)), [], filter);
  }
});

void test("each condition validateWSReuse refuses is an issue, in its order", () => {
  assert.deepEqual(issues((s) => (s.tcp.wssize = { enabled: true, window: 0, scale: 0 })), ["window"]);
  assert.deepEqual(issues((s) => (s.tcp.wssize = { enabled: false, window: 1, scale: 0 })), ["window"]);
  assert.deepEqual(issues((s) => (s.targets.ip = [])), ["targets"]);
  assert.deepEqual(issues((s) => (s.targets.sni_domains = ["example.com"])), ["domains"]);
  assert.deepEqual(issues((s) => (s.targets.geosite_categories = ["youtube"])), ["domains"]);
  assert.deepEqual(issues((s) => (s.targets.tls = "1.3")), ["tls"]);
  assert.deepEqual(issues((s) => (s.tcp.dport_filter = "8000-9000")), ["port"]);
  assert.deepEqual(issues((s) => (s.escalate = { to: "other" })), ["escalation"]);
  assert.deepEqual(
    issues((s) => {
      s.targets.domain_only = true;
      s.escalate = { to: "other" };
      s.tcp.dport_filter = "8443";
    }),
    ["domains", "port", "escalation"],
  );
});

void test("routing and duplication are left to the WSSIZE conflict alert", () => {
  assert.deepEqual(issues((s) => (s.routing.enabled = true)), []);
  assert.deepEqual(issues((s) => (s.tcp.duplicate = { enabled: true, count: 3 })), []);
});

void test("an engine condition makes reuse inactive, as WSReuseInactiveReason orders them", () => {
  assert.deepEqual(issues(() => {}, { tunEngine: true }), ["tun"]);
  assert.deepEqual(issues(() => {}, { tunEngine: true, skipSetup: true, queueMark: 0x48000 }), ["tun"]);
  assert.deepEqual(issues(() => {}, { skipSetup: true }), ["skip_setup"]);
  assert.deepEqual(issues(() => {}, { skipSetup: true, queueMark: 0x48000 }), ["skip_setup"]);
  assert.deepEqual(issues(() => {}, { queueMark: 0x48000 }), ["queue_mark"]);
  assert.deepEqual(issues(() => {}, { queueMark: 0x208000 }), ["queue_mark"]);
  assert.deepEqual(issues(() => {}, { queueMark: 0x8000 }), []);
  assert.deepEqual(issues(() => {}, { queueMark: 0x4008000 }), []);
});

void test("the engine warning is left out while Go refuses the set", () => {
  assert.deepEqual(issues((s) => (s.targets.tls = "1.3"), { tunEngine: true }), ["tls"]);
  assert.deepEqual(issues((s) => (s.routing.enabled = true), { skipSetup: true }), []);
  assert.deepEqual(
    issues((s) => (s.tcp.duplicate = { enabled: true, count: 3 }), { queueMark: 0x48000 }),
    [],
  );
});

void test("a disabled set or a set without reuse has no issue", () => {
  assert.deepEqual(
    issues(
      (s) => {
        s.enabled = false;
        s.targets.ip = [];
      },
      { tunEngine: true },
    ),
    [],
  );
  assert.deepEqual(
    issues(
      (s) => {
        s.tcp.wssize_reuse = { enabled: false, max_age_seconds: 120 };
        s.tcp.wssize = { enabled: true, window: 0, scale: 0 };
      },
      { skipSetup: true },
    ),
    [],
  );
});

void test("refusals stop the save and the engine reasons do not", () => {
  for (const issue of ["window", "targets", "domains", "tls", "port", "escalation"] as const) {
    assert.equal(isWSSizeReuseRefusal(issue), true, issue);
  }
  for (const issue of ["tun", "skip_setup", "queue_mark"] as const) {
    assert.equal(isWSSizeReuseRefusal(issue), false, issue);
  }
});
