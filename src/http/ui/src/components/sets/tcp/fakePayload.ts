import { B4SetConfig } from "@models/config";

export const sendsFake = (config: B4SetConfig) =>
  config.faking.sni ||
  (!!config.tcp.syn_fake && (config.tcp.syn_fake_len ?? 0) > 0) ||
  !!config.fragmentation.combo?.decoy_enabled;
