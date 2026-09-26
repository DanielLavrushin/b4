import { apiGet } from "@api/apiClient";
import { stripPort } from "@utils";

export interface IpInfo {
  ip: string;
  hostname?: string;
  city?: string;
  region?: string;
  country?: string;
  loc?: string;
  org?: string;
  postal?: string;
  timezone?: string;
}

export const fetchIpInfo = (ip: string) =>
  apiGet<IpInfo>(
    `/api/integration/ipinfo?ip=${encodeURIComponent(stripPort(ip))}`,
  );

export const ipInfoLocation = (info: IpInfo): string =>
  [info.city, info.region, info.country].filter(Boolean).join(", ");
