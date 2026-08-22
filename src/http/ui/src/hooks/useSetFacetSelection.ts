import { useCallback, useEffect, useMemo, useState } from "react";
import { FACET_ORDER, FacetKey } from "@components/sets/facets";
import { FacetToggleMode } from "@components/sets/SignalRail";

const STORAGE_KEY = "b4_sets_facets";

export type FacetSelection = Record<string, FacetKey>;

interface FacetState {
  open: FacetSelection;
  remembered: FacetSelection;
  compare: FacetKey | null;
}

const isFacetKey = (value: unknown): value is FacetKey =>
  typeof value === "string" && (FACET_ORDER as string[]).includes(value);

const loadSelection = (): FacetSelection => {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    const selection: FacetSelection = {};
    for (const [id, key] of Object.entries(parsed ?? {})) {
      if (isFacetKey(key)) selection[id] = key;
    }
    return selection;
  } catch {
    return {};
  }
};

const saveSelection = (selection: FacetSelection) => {
  try {
    if (Object.keys(selection).length === 0) {
      localStorage.removeItem(STORAGE_KEY);
      return;
    }
    localStorage.setItem(STORAGE_KEY, JSON.stringify(selection));
  } catch {
    /* storage unavailable */
  }
};

const pruneSelection = (
  selection: FacetSelection,
  known: Set<string>,
): FacetSelection | null => {
  const kept = Object.entries(selection).filter(([id]) => known.has(id));
  if (kept.length === Object.keys(selection).length) return null;
  return Object.fromEntries(kept);
};

export function useSetFacetSelection(setIds: string[]) {
  const [state, setState] = useState<FacetState>(() => {
    const stored = loadSelection();
    return { open: stored, remembered: stored, compare: null };
  });

  const knownKey = setIds.join(",");

  useEffect(() => {
    if (!knownKey) return;
    const known = new Set(knownKey.split(","));
    setState((prev) => {
      const open = pruneSelection(prev.open, known);
      const remembered = pruneSelection(prev.remembered, known);
      if (!open && !remembered) return prev;
      const next = {
        ...prev,
        open: open ?? prev.open,
        remembered: remembered ?? prev.remembered,
      };
      saveSelection(next.remembered);
      return next;
    });
  }, [knownKey]);

  const selectFacet = useCallback((setId: string, key: FacetKey) => {
    setState((prev) => {
      const open = { ...prev.open };
      if (open[setId] === key) delete open[setId];
      else open[setId] = key;
      saveSelection(open);
      return { open, remembered: open, compare: null };
    });
  }, []);

  const pickCompare = useCallback((key: FacetKey | null, ids: string[]) => {
    const open: FacetSelection = {};
    if (key) for (const id of ids) open[id] = key;
    saveSelection({});
    setState({ open, remembered: {}, compare: key });
  }, []);

  const toggleAll = useCallback(() => {
    setState((prev) => {
      if (Object.keys(prev.open).length > 0) {
        return { ...prev, open: {}, compare: null };
      }
      return { ...prev, open: prev.remembered, compare: null };
    });
  }, []);

  const toggle = useMemo<FacetToggleMode | null>(() => {
    if (Object.keys(state.open).length > 0) return "collapse";
    if (Object.keys(state.remembered).length > 0) return "expand";
    return null;
  }, [state.open, state.remembered]);

  return {
    open: state.open,
    compare: state.compare,
    toggle,
    selectFacet,
    pickCompare,
    toggleAll,
  };
}
