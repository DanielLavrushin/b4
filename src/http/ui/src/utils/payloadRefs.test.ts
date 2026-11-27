import { test } from "node:test";
import assert from "node:assert/strict";
import { B4SetConfig, FakingPayloadType } from "../models/config";
import { missingPayloadRefs } from "./payloadRefs";

const set = (fields: unknown) => fields as B4SetConfig;

const sets = [
  set({
    faking: { sni_type: FakingPayloadType.CAPTURE, payload_file: "tls_a.bin" },
    udp: { mode: "fake", fake_payload_file: "captures/quic_b.bin" },
  }),
  set({
    faking: { sni_type: FakingPayloadType.CAPTURE, payload_file: "tls_a.bin" },
    udp: { mode: "fake", fake_payload_file: "@builtin" },
  }),
  set({
    faking: { payload_file: "not-used.bin" },
    udp: { mode: "drop", fake_payload_file: "off.bin" },
  }),
];

void test("imported sets name each payload file this router lacks once", () => {
  assert.deepEqual(missingPayloadRefs(sets, []), [
    "tls_a.bin",
    "captures/quic_b.bin",
  ]);
  assert.deepEqual(missingPayloadRefs(sets, ["/etc/b4/captures/quic_b.bin"]), [
    "tls_a.bin",
  ]);
});

void test("a payload file is found by its name wherever it is stored", () => {
  assert.deepEqual(
    missingPayloadRefs(sets, ["C:\\b4\\tls_a.bin", "quic_b.bin"]),
    [],
  );
});
