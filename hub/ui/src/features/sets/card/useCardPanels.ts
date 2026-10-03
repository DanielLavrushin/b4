import { useCallback, useEffect, useMemo, useState } from "react";
import type { FacetKey, FacetToggleMode } from "@design";

type PanelMap = Record<string, string>;

interface PanelState {
  open: PanelMap;
  remembered: PanelMap;
  compare: FacetKey | null;
}

const prune = (map: PanelMap, known: Set<string>): PanelMap => {
  const kept = Object.entries(map).filter(([key]) => known.has(key));
  return kept.length === Object.keys(map).length ? map : Object.fromEntries(kept);
};

export function useCardPanels(keys: readonly string[]) {
  const [state, setState] = useState<PanelState>({ open: {}, remembered: {}, compare: null });
  const known = keys.join("\n");

  useEffect(() => {
    const present = new Set(known ? known.split("\n") : []);
    setState((prev) => {
      const open = prune(prev.open, present);
      const remembered = prune(prev.remembered, present);
      return open === prev.open && remembered === prev.remembered ? prev : { ...prev, open, remembered };
    });
  }, [known]);

  const panelOf = useCallback((key: string): string | null => state.open[key] ?? null, [state.open]);

  const setPanel = useCallback((key: string, panel: string | null) => {
    setState((prev) => {
      const open = { ...prev.open };
      if (panel) open[key] = panel;
      else delete open[key];
      return { open, remembered: open, compare: null };
    });
  }, []);

  const pickCompare = useCallback(
    (facet: FacetKey | null) => {
      const open: PanelMap = {};
      if (facet) for (const key of known ? known.split("\n") : []) open[key] = facet;
      setState({ open, remembered: {}, compare: facet });
    },
    [known],
  );

  const toggleAll = useCallback(() => {
    setState((prev) => (Object.keys(prev.open).length > 0 ? { ...prev, open: {}, compare: null } : { ...prev, open: prev.remembered, compare: null }));
  }, []);

  const toggle = useMemo<FacetToggleMode | null>(() => {
    if (Object.keys(state.open).length > 0) return "collapse";
    if (Object.keys(state.remembered).length > 0) return "expand";
    return null;
  }, [state.open, state.remembered]);

  return { panelOf, setPanel, compare: state.compare, pickCompare, toggle, toggleAll };
}
