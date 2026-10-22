import {
  createContext,
  use,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";

interface ChangedValues {
  before: unknown;
  after: unknown;
}

interface ChangedFieldsContextValue {
  changedPaths: ReadonlySet<string>;
  changedValues: ReadonlyMap<string, ChangedValues>;
  registerLabel: (path: string, label: ReactNode) => void;
  snapshotLabels: () => ReadonlyMap<string, ReactNode>;
  registerValueLabels: (path: string, entries: ReadonlyMap<string, string>) => void;
  snapshotValueLabels: () => ReadonlyMap<string, ReadonlyMap<string, string>>;
  scopeVersion: number;
}

const EMPTY_CHANGED_PATHS: ReadonlySet<string> = new Set();
const EMPTY_CHANGED_VALUES: ReadonlyMap<string, ChangedValues> = new Map();

const defaultChangedFields: ChangedFieldsContextValue = {
  changedPaths: EMPTY_CHANGED_PATHS,
  changedValues: EMPTY_CHANGED_VALUES,
  registerLabel: () => {},
  snapshotLabels: () => new Map(),
  registerValueLabels: () => {},
  snapshotValueLabels: () => new Map(),
  scopeVersion: 0,
};

const ChangedFieldsContext =
  createContext<ChangedFieldsContextValue | null>(null);

export function ChangedFieldsProvider({
  changedPaths,
  changedValues,
  scope,
  registry,
  valueRegistry,
  children,
}: Readonly<{
  changedPaths: ReadonlySet<string>;
  changedValues?: ReadonlyMap<string, ChangedValues>;
  scope: string;
  registry?: { current: Map<string, ReactNode> };
  valueRegistry?: { current: Map<string, ReadonlyMap<string, string>> };
  children: ReactNode;
}>) {
  const ownRef = useRef(new Map<string, ReactNode>());
  const labelsRef = registry ?? ownRef;
  const ownValueRef = useRef(new Map<string, ReadonlyMap<string, string>>());
  const valueLabelsRef = valueRegistry ?? ownValueRef;
  const scopeRef = useRef(scope);
  const [scopeVersion, setScopeVersion] = useState(0);
  if (scopeRef.current !== scope) {
    scopeRef.current = scope;
    labelsRef.current.clear();
    valueLabelsRef.current.clear();
    setScopeVersion((v) => v + 1);
  }

  const registerLabel = useCallback<ChangedFieldsContextValue["registerLabel"]>(
    (path, label) => {
      labelsRef.current.set(path, label);
    },
    [labelsRef],
  );
  const snapshotLabels = useCallback<
    ChangedFieldsContextValue["snapshotLabels"]
  >(() => new Map(labelsRef.current), [labelsRef]);
  const registerValueLabels = useCallback<
    ChangedFieldsContextValue["registerValueLabels"]
  >(
    (path, entries) => {
      const prev = valueLabelsRef.current.get(path);
      valueLabelsRef.current.set(path, new Map([...(prev ?? []), ...entries]));
    },
    [valueLabelsRef],
  );
  const snapshotValueLabels = useCallback<
    ChangedFieldsContextValue["snapshotValueLabels"]
  >(() => new Map(valueLabelsRef.current), [valueLabelsRef]);
  const value = useMemo(
    () => ({
      changedPaths,
      changedValues: changedValues ?? EMPTY_CHANGED_VALUES,
      registerLabel,
      snapshotLabels,
      registerValueLabels,
      snapshotValueLabels,
      scopeVersion,
    }),
    [
      changedPaths,
      changedValues,
      registerLabel,
      snapshotLabels,
      registerValueLabels,
      snapshotValueLabels,
      scopeVersion,
    ],
  );

  return (
    <ChangedFieldsContext value={value}>{children}</ChangedFieldsContext>
  );
}

// Object-valued controls (e.g. dns.pins) diff into child leaves, so a parent
// field must light up when any descendant changed.
const changedInPath = (
  paths: ReadonlySet<string>,
  path: string,
): boolean => {
  if (paths.has(path)) return true;
  const prefix = path + ".";
  for (const p of paths) if (p.startsWith(prefix)) return true;
  return false;
};

export function useChangedField(path?: string, label?: ReactNode): boolean {
  const context = use(ChangedFieldsContext) ?? defaultChangedFields;
  const { registerLabel } = context;

  useEffect(() => {
    if (path && label !== undefined) {
      registerLabel(path, label);
    }
  }, [registerLabel, path, label]);

  return Boolean(path && changedInPath(context.changedPaths, path));
}

export function useChangedArrayItem(
  path?: string,
  item?: string,
  label?: ReactNode,
): boolean {
  const context = use(ChangedFieldsContext) ?? defaultChangedFields;
  const { registerLabel } = context;
  const childPath = path && item ? `${path}.${item}` : undefined;

  useEffect(() => {
    if (childPath && label !== undefined) {
      registerLabel(childPath, label);
    }
  }, [registerLabel, childPath, label]);

  if (!path || !item) return false;
  const leaf = context.changedValues.get(path);
  if (!leaf) return false;
  const has = (value: unknown): boolean =>
    Array.isArray(value) && (value as unknown[]).includes(item);
  return has(leaf.after) !== has(leaf.before);
}

export function useChangedFields(): {
  changedPaths: ReadonlySet<string>;
  changedValues: ReadonlyMap<string, ChangedValues>;
  registerLabel: (path: string, label: ReactNode) => void;
  snapshotLabels: () => ReadonlyMap<string, ReactNode>;
  registerValueLabels: (path: string, entries: ReadonlyMap<string, string>) => void;
  snapshotValueLabels: () => ReadonlyMap<string, ReadonlyMap<string, string>>;
  scopeVersion: number;
} {
  return use(ChangedFieldsContext) ?? defaultChangedFields;
}
