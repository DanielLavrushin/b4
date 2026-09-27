import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router";
import type { SortDir } from "./sort";

interface TableDefaults {
  sort?: string;
  dir?: SortDir;
  pageSize?: number;
}

export const PAGE_SIZES = [25, 50, 100, 200];

export function useTableState(defaults: TableDefaults = {}) {
  const [params, setParams] = useSearchParams();
  const sort = params.get("sort") ?? defaults.sort ?? "";
  const dir: SortDir = params.get("dir") === "asc" ? "asc" : params.get("dir") === "desc" ? "desc" : (defaults.dir ?? "desc");
  const page = Math.max(0, Number(params.get("page") ?? "0") || 0);
  const rawSize = Number(params.get("size") ?? "");
  const pageSize = PAGE_SIZES.includes(rawSize) ? rawSize : (defaults.pageSize ?? 50);
  const urlQ = params.get("q") ?? "";
  const [q, setLocalQ] = useState(urlQ);

  const update = useCallback(
    (changes: Record<string, string | null>, resetPage = true) => {
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          Object.entries(changes).forEach(([k, v]) => {
            if (v === null || v === "") next.delete(k);
            else next.set(k, v);
          });
          if (resetPage && !("page" in changes)) next.delete("page");
          return next;
        },
        { replace: true },
      );
    },
    [setParams],
  );

  useEffect(() => {
    setLocalQ(urlQ);
  }, [urlQ]);

  useEffect(() => {
    if (q === urlQ) return;
    const handle = setTimeout(() => update({ q: q.trim() === "" ? null : q }), 250);
    return () => clearTimeout(handle);
  }, [q, urlQ, update]);

  return useMemo(
    () => ({
      q,
      query: urlQ.trim().toLowerCase(),
      exact: urlQ.trim(),
      setQ: setLocalQ,
      sort,
      dir,
      setSort: (by: string, d: SortDir) => update({ sort: by, dir: d }),
      page,
      setPage: (p: number) => update({ page: p > 0 ? String(p) : null }, false),
      pageSize,
      setPageSize: (n: number) => update({ size: String(n) }),
      param: (name: string) => params.get(name) ?? "",
      setParam: (name: string, value: string | null) => update({ [name]: value }),
    }),
    [q, urlQ, sort, dir, page, pageSize, params, update],
  );
}

export type TableState = ReturnType<typeof useTableState>;
