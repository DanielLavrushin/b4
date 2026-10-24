import { useCallback, useMemo, useRef } from "react";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { diffConfigLeaves } from "@utils";
import {
  buildChangeItems,
  groupChangeItems,
  type ChangeGroupDef,
  type SubDef,
} from "@common/UnsavedChangesDialog";
import { labelKeyForPath } from "@common/dialogLabels";

export interface ChangeTrackerOptions {
  draft: Record<string, unknown> | null;
  baseline: Record<string, unknown> | null;
  ignoredRootKeys: ReadonlySet<string>;
  scope: string;
}

export const useChangeTracker = (o: ChangeTrackerOptions) => {
  const changedLeaves = useMemo(() => {
    if (!o.draft || !o.baseline) return [];
    return diffConfigLeaves(o.draft, o.baseline, {
      ignoredRootKeys: o.ignoredRootKeys,
    });
  }, [o.draft, o.baseline, o.ignoredRootKeys]);
  const changedPaths = useMemo<ReadonlySet<string>>(
    () => new Set<string>(changedLeaves.map((leaf) => leaf.path)),
    [changedLeaves],
  );
  const changedValues = useMemo(
    () =>
      new Map<string, { before: unknown; after: unknown }>(
        changedLeaves.map((leaf) => [
          leaf.path,
          { before: leaf.before, after: leaf.after },
        ]),
      ),
    [changedLeaves],
  );
  const registry = useRef(new Map<string, ReactNode>());
  const valueRegistry = useRef(
    new Map<string, ReadonlyMap<string, string>>(),
  );
  return {
    changedLeaves,
    changedPaths,
    changedValues,
    providerProps: {
      changedPaths,
      changedValues,
      scope: o.scope,
      registry,
      valueRegistry,
    },
  };
};

export interface ChangeItemsOptions {
  changedLeaves: { path: string; before: unknown; after: unknown }[];
  registry: { current: Map<string, ReactNode> };
  valueRegistry: { current: Map<string, ReadonlyMap<string, string>> };
  extraValueLabels?: ReadonlyArray<
    readonly [string, ReadonlyMap<string, string>]
  >;
  groups: ChangeGroupDef[];
  subDefs: SubDef[];
  dialogsOpen: boolean;
}

export const useChangeItems = (o: ChangeItemsOptions) => {
  const { t } = useTranslation();
  const snapshotLabels = useCallback(
    () => new Map(o.registry.current),
    [o.registry],
  );
  const staticLabels = useMemo(() => {
    const map = new Map<string, ReactNode>();
    for (const leaf of o.changedLeaves) {
      const key = labelKeyForPath(leaf.path);
      if (key !== undefined) map.set(leaf.path, t(key));
    }
    return map;
  }, [o.changedLeaves, t]);
  const changeItems = useMemo(() => {
    if (!o.dialogsOpen) return [];
    return buildChangeItems(
      o.changedLeaves,
      new Map([...staticLabels, ...snapshotLabels()]),
      new Map([...o.valueRegistry.current, ...(o.extraValueLabels ?? [])]),
    );
  }, [
    o.dialogsOpen,
    o.changedLeaves,
    staticLabels,
    snapshotLabels,
    o.valueRegistry,
    o.extraValueLabels,
  ]);
  const changeGroups = useMemo(
    () =>
      o.dialogsOpen
        ? groupChangeItems(changeItems, o.groups, o.subDefs).map((group) => ({
            ...group,
            label: t(group.label),
          }))
        : [],
    [o.dialogsOpen, changeItems, o.groups, o.subDefs, t],
  );
  return { changeItems, changeGroups };
};
