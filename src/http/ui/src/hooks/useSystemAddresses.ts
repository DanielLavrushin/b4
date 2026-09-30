import { useQuery, useQueryClient } from "@tanstack/react-query";
import { systemApi } from "@api/settings";

const systemAddressesRoot = ["system", "addresses"] as const;

export const systemAddressesKey = (probePublic: boolean) =>
  [...systemAddressesRoot, probePublic ? "public" : "local"] as const;

export function useSystemAddresses(enabled: boolean, probePublic: boolean) {
  return useQuery({
    queryKey: systemAddressesKey(probePublic),
    queryFn: () => systemApi.addresses(probePublic),
    enabled,
    staleTime: 5 * 1000,
    retry: false,
  });
}

export function useSystemAddressesInvalidate() {
  const client = useQueryClient();
  return () => client.invalidateQueries({ queryKey: systemAddressesRoot });
}
