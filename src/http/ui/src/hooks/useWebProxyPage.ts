import {
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { mtprotoApi } from "@api/mtproto";

export const webProxyPageKey = ["mtproto", "web-proxy", "page"] as const;

export function useWebProxyPage(enabled = true) {
  return useQuery({
    queryKey: webProxyPageKey,
    queryFn: mtprotoApi.webProxyPage,
    enabled,
    retry: false,
  });
}

export function useUploadWebProxyPage() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: (file: File) => mtprotoApi.uploadWebProxyPage(file),
    onSuccess: () => client.invalidateQueries({ queryKey: webProxyPageKey }),
  });
}

export function useRemoveWebProxyPage() {
  const client = useQueryClient();
  return useMutation({
    mutationFn: () => mtprotoApi.removeWebProxyPage(),
    onSuccess: () => client.invalidateQueries({ queryKey: webProxyPageKey }),
  });
}
