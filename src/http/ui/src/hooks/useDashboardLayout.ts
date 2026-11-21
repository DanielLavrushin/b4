import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { apiGet, apiPut } from "@api/apiClient";
import { DASHBOARD_PANELS } from "@components/dashboard/registry";
import type { Arrangement } from "@components/dashboard/layoutRows";
import {
  LAYOUT_VERSION,
  savedLayouts,
  type StoredDashboard,
  type StoredLayout,
} from "@components/dashboard/savedLayout";

const STORAGE_KEY = "b4_dashboard_layout";
const LAYOUT_ENDPOINT = "/api/ui/dashboard";
const SAVE_DEBOUNCE_MS = 800;

const { defaultSpanOf, normalizeSpans, emptyLayout, normalize, isCustomized } =
  savedLayouts(DASHBOARD_PANELS);

export { defaultSpanOf };

const loadLayout = (): StoredLayout => {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return emptyLayout();
    const parsed = JSON.parse(raw) as Partial<StoredLayout>;
    if (parsed?.v !== LAYOUT_VERSION) return emptyLayout();
    return normalize(parsed);
  } catch {
    return emptyLayout();
  }
};

const saveLocal = (layout: StoredLayout): void => {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(layout));
  } catch {
    return;
  }
};

export function useDashboardLayout() {
  const [layout, setLayout] = useState<StoredLayout>(loadLayout);
  const [hydrated, setHydrated] = useState(false);
  const dirty = useRef(false);

  useEffect(() => {
    let cancelled = false;
    apiGet<StoredDashboard>(LAYOUT_ENDPOINT)
      .then((remote) => {
        if (cancelled || dirty.current) return;
        const next = normalize(remote ?? {});
        if (isCustomized(next)) setLayout(next);
      })
      .catch(() => undefined)
      .finally(() => {
        if (!cancelled) setHydrated(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    saveLocal(layout);
    if (!hydrated || !dirty.current) return;
    const timer = setTimeout(() => {
      void apiPut<StoredDashboard>(LAYOUT_ENDPOINT, {
        order: layout.order,
        hidden: layout.hidden,
        spans: layout.spans,
        breaks: layout.breaks,
      }).catch(() => undefined);
    }, SAVE_DEBOUNCE_MS);
    return () => clearTimeout(timer);
  }, [layout, hydrated]);

  const mutate = useCallback(
    (updater: (prev: StoredLayout) => StoredLayout) => {
      dirty.current = true;
      setLayout(updater);
    },
    [],
  );

  const hidden = useMemo(() => new Set(layout.hidden), [layout.hidden]);

  const arrange = useCallback(
    (change: (current: Arrangement) => Arrangement) => {
      mutate((prev) => {
        const next = change(prev);
        if (next === prev) return prev;
        return {
          ...prev,
          order: [...next.order],
          breaks: [...next.breaks],
          spans: normalizeSpans(next.spans),
        };
      });
    },
    [mutate],
  );

  const setHidden = useCallback(
    (id: string, value: boolean) => {
      mutate((prev) => {
        const next = prev.hidden.filter((entry) => entry !== id);
        if (value) next.push(id);
        return { ...prev, hidden: next };
      });
    },
    [mutate],
  );

  const reset = useCallback(() => mutate(emptyLayout), [mutate]);

  const customized = useMemo(() => isCustomized(layout), [layout]);

  return {
    order: layout.order,
    breaks: layout.breaks,
    hidden,
    spans: layout.spans,
    arrange,
    setHidden,
    reset,
    customized,
  };
}
