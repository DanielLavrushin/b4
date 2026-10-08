import {
  createContext,
  use,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  type ReactNode,
} from "react";

interface ChangedFieldsContextValue {
  changedPaths: ReadonlySet<string>;
  registerLabel: (path: string, label: ReactNode) => void;
  snapshotLabels: () => ReadonlyMap<string, ReactNode>;
}

const EMPTY_CHANGED_PATHS: ReadonlySet<string> = new Set();

const defaultChangedFields: ChangedFieldsContextValue = {
  changedPaths: EMPTY_CHANGED_PATHS,
  registerLabel: () => {},
  snapshotLabels: () => new Map(),
};

const ChangedFieldsContext =
  createContext<ChangedFieldsContextValue | null>(null);

export function ChangedFieldsProvider({
  changedPaths,
  scope,
  registry,
  children,
}: Readonly<{
  changedPaths: ReadonlySet<string>;
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
    () => ({ changedPaths, registerLabel, snapshotLabels }),
    [changedPaths, registerLabel, snapshotLabels],
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

export function useChangedFields(): {
  changedPaths: ReadonlySet<string>;
  snapshotLabels: () => ReadonlyMap<string, ReactNode>;
} {
  return use(ChangedFieldsContext) ?? defaultChangedFields;
}
