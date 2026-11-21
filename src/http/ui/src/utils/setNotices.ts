import type { B4SetConfig } from "@models/config";

const listed = (values?: string[] | null) => (values?.length ?? 0) > 0;

export const domainOnlyRoutesNothing = (set: B4SetConfig): boolean =>
  !!set.targets.domain_only &&
  !!set.routing?.enabled &&
  set.routing.mode !== "block" &&
  (listed(set.targets.sni_domains) || listed(set.targets.geosite_categories));
