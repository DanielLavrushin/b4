import { test } from "node:test";
import assert from "node:assert/strict";
import type { B4SetConfig, IPBlockDetectConfig } from "../models/config";
import {
  collectedTakes,
  domainOnlyRoutesNothing,
  setAddsBans,
} from "./setNotices";
import type { WSSizeReuseEnv } from "./wssizeReuse";

const nfqueue: WSSizeReuseEnv = { tunEngine: false, skipSetup: false, queueMark: 0 };

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
    routing: {
      enabled: false,
      mode: "interface",
      egress_interface: "",
      upstream: { host: "", port: 0, udp: false },
    },
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

const detecting = (
  detect: Partial<IPBlockDetectConfig> = {},
  change: (set: B4SetConfig) => void = () => {},
): B4SetConfig =>
  shaped((s) => {
    s.tcp.ip_block_detect = {
      action: "block",
      enabled: true,
      retransmit_threshold: 3,
      timeout_ms: 3000,
      cache_blocked_ips: true,
      syn_detect: true,
      syn_threshold: 3,
      heal_dns: false,
      heal_ttl_sec: 60,
      ...detect,
    };
    change(s);
  });

void test("an enabled set with blocking detection and the cache on adds bans", () => {
  assert.equal(setAddsBans(detecting()), true);
  assert.equal(setAddsBans(detecting({ action: undefined })), true);
  assert.equal(
    setAddsBans(
      detecting({}, (s) => {
        s.routing.enabled = true;
        s.routing.mode = "interface";
        s.routing.egress_interface = "wg0";
      }),
    ),
    true,
  );
});

void test("a set that is off, does not detect or does not cache adds no bans", () => {
  assert.equal(setAddsBans(baseSet()), false);
  assert.equal(setAddsBans(detecting({ enabled: false })), false);
  assert.equal(
    setAddsBans(detecting({}, (s) => (s.enabled = false))),
    false,
  );
  assert.equal(setAddsBans(detecting({ cache_blocked_ips: false })), false);
});

void test("a detector that only collects adds no bans", () => {
  assert.equal(setAddsBans(detecting({ action: "collect" })), false);
});

void test("a set routed through a proxy hands its connections off before detection", () => {
  for (const mode of ["proxy", "mtproto-ws"] as const) {
    assert.equal(
      setAddsBans(
        detecting({}, (s) => {
          s.routing.enabled = true;
          s.routing.mode = mode;
        }),
      ),
      false,
      mode,
    );
  }
});

const collecting = (change: (set: B4SetConfig) => void): B4SetConfig =>
  shaped((s) => {
    s.targets.use_collected_ips = true;
    s.targets.sni_domains = [];
    change(s);
  });

const reusing = (change: (set: B4SetConfig) => void = () => {}): B4SetConfig =>
  collecting((s) => {
    s.tcp.wssize = { enabled: true, window: 1, scale: 0 };
    s.tcp.wssize_reuse = { enabled: true, max_age_seconds: 300 };
    change(s);
  });

const routed = (
  mode: B4SetConfig["routing"]["mode"],
  change: (set: B4SetConfig) => void = () => {},
): B4SetConfig =>
  collecting((s) => {
    s.routing.enabled = true;
    s.routing.mode = mode;
    change(s);
  });

void test("a set routed to an interface takes collected addresses once an output interface is chosen", () => {
  assert.equal(collectedTakes(routed("interface"), nfqueue), null);
  assert.equal(
    collectedTakes(
      routed("interface", (s) => (s.routing.egress_interface = "wg0")),
      nfqueue,
    ),
    "all",
  );
});

void test("a proxy set takes TCP to collected addresses, and UDP only when it carries UDP", () => {
  assert.equal(collectedTakes(routed("proxy"), nfqueue), "tcp");
  assert.equal(
    collectedTakes(
      routed("proxy", (s) => (s.routing.upstream.udp = true)),
      nfqueue,
    ),
    "all",
  );
});

void test("a Telegram WebSocket set takes TCP to collected addresses", () => {
  assert.equal(collectedTakes(routed("mtproto-ws"), nfqueue), "tcp");
});

void test("a block set takes nothing", () => {
  assert.equal(collectedTakes(routed("block"), nfqueue), null);
});

void test("reuse takes collected addresses only while it is active", () => {
  assert.equal(collectedTakes(reusing(), nfqueue), "reuse");
  assert.equal(
    collectedTakes(reusing(), { ...nfqueue, tunEngine: true }),
    null,
  );
  assert.equal(
    collectedTakes(reusing(), { ...nfqueue, skipSetup: true }),
    null,
  );
  assert.equal(
    collectedTakes(reusing(), { ...nfqueue, queueMark: 0x40000 }),
    null,
  );
  assert.equal(
    collectedTakes(
      reusing((s) => (s.tcp.wssize = { enabled: false, window: 1, scale: 0 })),
      nfqueue,
    ),
    null,
  );
});

void test("a disabled set, or one that does not use collected IPs, takes nothing", () => {
  assert.equal(
    collectedTakes(
      routed("proxy", (s) => (s.enabled = false)),
      nfqueue,
    ),
    null,
  );
  assert.equal(
    collectedTakes(
      reusing((s) => (s.enabled = false)),
      nfqueue,
    ),
    null,
  );
  assert.equal(
    collectedTakes(
      routed("proxy", (s) => (s.targets.use_collected_ips = false)),
      nfqueue,
    ),
    null,
  );
});
