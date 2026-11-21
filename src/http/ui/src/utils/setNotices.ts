import type { B4SetConfig } from "@models/config";
import { wssizeReuseIssues, type WSSizeReuseEnv } from "./wssizeReuse";

const listed = (values?: string[] | null) => (values?.length ?? 0) > 0;

export type CollectedTakes = "all" | "tcp" | "reuse";

const routedTakes = (routing: B4SetConfig["routing"]): CollectedTakes | null => {
  switch (routing.mode) {
    case "block":
      return null;
    case "mtproto-ws":
      return "tcp";
    case "proxy":
      return routing.upstream?.udp ? "all" : "tcp";
    default:
      return routing.egress_interface ? "all" : null;
  }
};

export const collectedTakes = (
  set: B4SetConfig,
  env: WSSizeReuseEnv,
): CollectedTakes | null => {
  if (!set.enabled || !set.targets.use_collected_ips) return null;
  if (set.routing?.enabled) return routedTakes(set.routing);
  if (!set.tcp.wssize_reuse?.enabled) return null;
  return wssizeReuseIssues(set, env).length === 0 ? "reuse" : null;
};

export const domainOnlyRoutesNothing = (set: B4SetConfig): boolean =>
  !!set.targets.domain_only &&
  !!set.routing?.enabled &&
  set.routing.mode !== "block" &&
  (listed(set.targets.sni_domains) || listed(set.targets.geosite_categories));
