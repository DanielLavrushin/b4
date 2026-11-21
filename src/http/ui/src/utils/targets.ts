import type { TargetsConfig } from "@models/config";

type IpTargets = Partial<
  Pick<TargetsConfig, "use_collected_ips" | "ip" | "geoip_categories" | "asns">
>;

const listed = (values?: string[]) => (values?.length ?? 0) > 0;

export const declaresIpTargets = (targets?: IpTargets): boolean =>
  !!targets?.use_collected_ips ||
  listed(targets?.ip) ||
  listed(targets?.geoip_categories) ||
  listed(targets?.asns);

export const mssClampIpScope = (targets?: IpTargets): boolean =>
  declaresIpTargets(targets);
