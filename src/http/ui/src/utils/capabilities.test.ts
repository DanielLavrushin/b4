import { test } from "node:test";
import assert from "node:assert/strict";
import type { DiagCapability } from "../models/sysinfo";
import { capabilityHints } from "./capabilities";

const queue: DiagCapability = {
  name: "packet_queue",
  available: false,
  missing: ["xt_NFQUEUE"],
  packages: ["kmod-ipt-nfqueue"],
  hint: "keenetic_netfilter",
};

void test("an unavailable packet queue asks for its packages in NFQUEUE mode", () => {
  assert.deepEqual(capabilityHints(queue, "nfqueue"), {
    install: true,
    keenetic: true,
    notNeeded: false,
  });
});

void test("TUN mode says NFQUEUE is not needed instead of asking to install it", () => {
  assert.deepEqual(capabilityHints(queue, "tun"), {
    install: false,
    keenetic: false,
    notNeeded: true,
  });
});

void test("TUN mode still asks for what the other capabilities miss", () => {
  const tproxy: DiagCapability = { ...queue, name: "transparent_proxy" };
  assert.deepEqual(capabilityHints(tproxy, "tun"), {
    install: true,
    keenetic: true,
    notNeeded: false,
  });
});

void test("an available capability shows no hint in either mode", () => {
  for (const mode of ["nfqueue", "tun"]) {
    assert.deepEqual(capabilityHints({ ...queue, available: true }, mode), {
      install: false,
      keenetic: false,
      notNeeded: false,
    });
  }
});

void test("no packages means no install line", () => {
  assert.equal(
    capabilityHints({ ...queue, packages: [] }, "nfqueue").install,
    false,
  );
});
