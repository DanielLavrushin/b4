import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { setsApi } from "@api/sets";
import { SetDomainMatch } from "@models/sets";

const CHECK_DOMAIN_BATCH = 32;
const hostMatchesRoot = ["sets", "host-matches"] as const;

const checkHosts = async (hosts: string[]): Promise<SetDomainMatch[]> => {
  const batches: string[][] = [];
  for (let i = 0; i < hosts.length; i += CHECK_DOMAIN_BATCH) {
    batches.push(hosts.slice(i, i + CHECK_DOMAIN_BATCH));
  }
  const lists = await Promise.all(
    batches.map((batch) => setsApi.checkDomain(batch.join(","))),
  );
  return lists.flatMap((list) => (Array.isArray(list) ? list : []));
};

export function useHostMatches(hosts: string[], enabled = true) {
  const query = useQuery({
    queryKey: [...hostMatchesRoot, hosts],
    queryFn: () => checkHosts(hosts),
    enabled: enabled && hosts.length > 0,
    staleTime: 0,
    retry: false,
    refetchOnWindowFocus: false,
  });
  return {
    matches: query.isError ? [] : (query.data ?? []),
    loading: query.isLoading,
    checking: query.isFetching,
    failed: query.isError,
  };
}

export function useHostMatchesInvalidate() {
  const client = useQueryClient();
  return useCallback(
    () => client.invalidateQueries({ queryKey: hostMatchesRoot }),
    [client],
  );
}
