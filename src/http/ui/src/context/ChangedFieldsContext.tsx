import {
  createContext,
  use,
  useCallback,
  useEffect,
  useMemo,
  useRef,
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
}

const EMPTY_CHANGED_PATHS: ReadonlySet<string> = new Set();
const EMPTY_CHANGED_VALUES: ReadonlyMap<string, ChangedValues> = new Map();

const defaultChangedFields: ChangedFieldsContextValue = {
  changedPaths: EMPTY_CHANGED_PATHS,
  changedValues: EMPTY_CHANGED_VALUES,
  registerLabel: () => {},
  snapshotLabels: () => new Map(),
};

const ChangedFieldsContext =
  createContext<ChangedFieldsContextValue | null>(null);

export function ChangedFieldsProvider({
  changedPaths,
  changedValues,
  scope,
  registry,
  children,
}: Readonly<{
  changedPaths: ReadonlySet<string>;
  changedValues?: ReadonlyMap<string, ChangedValues>;
  scope: string;
  registry?: { current: Map<string, ReactNode> };
  children: ReactNode;
}>) {
  const ownRef = useRef(new Map<string, ReactNode>());
  const labelsRef = registry ?? ownRef;
  const scopeRef = useRef(scope);
  if (scopeRef.current !== scope) {
    scopeRef.current = scope;
    labelsRef.current.clear();
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
  const value = useMemo(
    () => ({
      changedPaths,
      changedValues: changedValues ?? EMPTY_CHANGED_VALUES,
      registerLabel,
      snapshotLabels,
    }),
    [changedPaths, changedValues, registerLabel, snapshotLabels],
  );

  return (
    <ChangedFieldsContext value={value}>{children}</ChangedFieldsContext>
  );
}

export function useChangedField(path?: string, label?: ReactNode): boolean {
  const context = use(ChangedFieldsContext) ?? defaultChangedFields;

  useEffect(() => {
    if (path && label !== undefined) {
      context.registerLabel(path, label);
    }
  }, [context, path, label]);

  return Boolean(path && context.changedPaths.has(path));
}

export function useChangedArrayItem(
  path?: string,
  item?: string,
  label?: ReactNode,
): boolean {
  const context = use(ChangedFieldsContext) ?? defaultChangedFields;
  const childPath = path && item ? `${path}.${item}` : undefined;

  useEffect(() => {
    if (childPath && label !== undefined) {
      context.registerLabel(childPath, label);
    }
  }, [context, childPath, label]);

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
  snapshotLabels: () => ReadonlyMap<string, ReactNode>;
} {
  return use(ChangedFieldsContext) ?? defaultChangedFields;
}
