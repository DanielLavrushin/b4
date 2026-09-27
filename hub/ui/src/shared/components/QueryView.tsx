import { Box } from "@mui/material";
import type { ReactNode } from "react";
import type { UseQueryResult } from "@tanstack/react-query";
import { ErrorState, Loading } from "./States";

interface QueryViewProps<T> {
  query: UseQueryResult<T>;
  children: (data: T) => ReactNode;
  loading?: ReactNode;
}

export function QueryView<T>({ query, children, loading }: QueryViewProps<T>) {
  const retry = () => void query.refetch();
  if (query.data === undefined) {
    if (query.error) return <ErrorState error={query.error} onRetry={retry} />;
    return <>{loading ?? <Loading />}</>;
  }
  return (
    <>
      {query.error && (
        <Box sx={{ mb: 2 }}>
          <ErrorState error={query.error} onRetry={retry} compact />
        </Box>
      )}
      {children(query.data)}
    </>
  );
}
