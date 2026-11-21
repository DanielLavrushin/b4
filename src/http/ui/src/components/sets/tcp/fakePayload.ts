import { B4SetConfig, FakingPayloadType } from "@models/config";
import type { ChosenPayloadFile } from "../payloadFile";

export function sendsComboFragments(config: B4SetConfig): boolean {
  const pool = config.fragmentation.strategy_pool ?? [];
  const strategies = pool.length > 0 ? pool : [config.fragmentation.strategy];
  return strategies.some((s) => s === "combo" || s === "hybrid");
}

export const sendsSynOrDecoyFake = (config: B4SetConfig) =>
  (!!config.tcp.syn_fake && (config.tcp.syn_fake_len ?? 0) > 0) ||
  (!!config.fragmentation.combo?.decoy_enabled && sendsComboFragments(config));

export const sendsFake = (config: B4SetConfig) =>
  config.faking.sni || sendsSynOrDecoyFake(config);

export function tlsModChangesTheFake(
  config: B4SetConfig,
  chosen: ChosenPayloadFile,
): boolean {
  switch (config.faking.sni_type) {
    case FakingPayloadType.DEFAULT:
    case FakingPayloadType.DEFAULT2:
    case FakingPayloadType.DOMAIN:
      return true;
    case FakingPayloadType.CAPTURE:
      return chosen.state === "listed"
        ? chosen.capture.facts?.kind === "tls_client_hello"
        : chosen.state !== "unlisted" ||
            chosen.tooLarge ||
            chosen.facts?.kind === "tls_client_hello";
    case FakingPayloadType.INVERTED:
      return sendsSynOrDecoyFake(config);
    default:
      return false;
  }
}
