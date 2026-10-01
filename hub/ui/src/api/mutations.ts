import { useMutation, useQueryClient, type QueryKey } from "@tanstack/react-query";

interface AdminMutationOptions<TVariables, TResult> {
  mutationFn: (variables: TVariables) => Promise<TResult>;
  invalidates: (variables: TVariables, result: TResult | undefined) => readonly QueryKey[];
  removes?: (variables: TVariables) => readonly QueryKey[];
}

export function useAdminMutation<TVariables, TResult>({ mutationFn, invalidates, removes }: AdminMutationOptions<TVariables, TResult>) {
  const client = useQueryClient();
  return useMutation({
    mutationFn,
    onSettled: (result, _error, variables) => {
      removes?.(variables).forEach((queryKey) => client.removeQueries({ queryKey, exact: true }));
      invalidates(variables, result).forEach((queryKey) => {
        void client.invalidateQueries({ queryKey });
      });
    },
  });
}
