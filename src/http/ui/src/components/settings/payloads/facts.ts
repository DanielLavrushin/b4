import type { TFunction } from "i18next";
import type {
  BuiltinPayload,
  Capture,
  PayloadFacts,
  PayloadInspection,
  PayloadKind,
  PayloadProtocol,
  PayloadWarning,
} from "@models/capture";

export const asProtocol = (protocol: string): PayloadProtocol =>
  protocol === "quic" ? "quic" : "tls";

export const kindProtocol = (kind: PayloadKind): PayloadProtocol =>
  kind === "tls_client_hello" ? "tls" : "quic";

export function bytesLabel(t: TFunction, size: number): string {
  return t("settings.Capture.bytes", {
    count: size,
    size: size.toLocaleString(),
  });
}

export function kindLabel(t: TFunction, facts: PayloadFacts): string {
  if (facts.kind === "quic_initial" && facts.quic_version) {
    return t("settings.Capture.kinds.quic_initial_version", {
      version: facts.quic_version,
    });
  }
  return t(`settings.Capture.kinds.${facts.kind}`);
}

const builtinLabelKeys: Record<string, string> = {
  google: "sets.faking.fakeSni.payloadGoogle",
  vk: "sets.faking.fakeSni.payloadVk",
  stun: "sets.faking.fakeSni.payloadStun",
  quic1: "sets.udp.fakePayloadFilePreset1",
  quic2: "sets.udp.fakePayloadFilePreset2",
};

const knownSources = new Set(["youtubeunblock", "youtubeunblock_derived", "zapret"]);

export function builtinLabel(t: TFunction, builtin: BuiltinPayload): string {
  const key = builtinLabelKeys[builtin.id];
  return key ? t(key) : builtin.id;
}

export function builtinSource(t: TFunction, builtin: BuiltinPayload): string {
  return knownSources.has(builtin.source)
    ? t(`settings.Capture.sources.${builtin.source}`)
    : t("settings.Capture.sources.unknown");
}

export function captureWarnings(capture: Capture): PayloadWarning[] {
  const warnings = capture.warnings ?? [];
  return capture.missing && !warnings.includes("missing")
    ? ["missing", ...warnings]
    : warnings;
}

export function inspectionWarnings(
  result: PayloadInspection,
  protocol: PayloadProtocol,
): PayloadWarning[] {
  if (result.size === 0) return ["empty"];
  const out: PayloadWarning[] = [];
  if (result.fits && result.fits[protocol] === false) out.push("too_large");
  const kind = result.facts?.kind;
  if (kind === "other") out.push("unrecognized");
  else if (kind && kindProtocol(kind) !== protocol) out.push("kind_mismatch");
  if ((result.facts?.faults ?? []).length > 0) out.push("malformed");
  return out;
}

export function warningText(
  t: TFunction,
  code: PayloadWarning,
  protocol: PayloadProtocol,
  facts?: PayloadFacts | null,
): string {
  switch (code) {
    case "missing":
      return protocol === "quic"
        ? t("settings.Capture.missingFileQuic")
        : t("settings.Capture.missingFileTls");
    case "too_large":
      return t(`settings.Capture.warnings.too_large_${protocol}`);
    case "kind_mismatch":
      return t(`settings.Capture.warnings.kind_mismatch_${protocol}`, {
        kind: facts ? kindLabel(t, facts) : "",
      });
    case "malformed":
      return t("settings.Capture.warnings.malformed", {
        fields: (facts?.faults ?? []).join(", "),
      });
    default:
      return t(`settings.Capture.warnings.${code}`);
  }
}
