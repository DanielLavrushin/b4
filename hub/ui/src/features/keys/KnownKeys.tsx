import { createContext, use, useMemo, type ReactNode } from "react";
import type { NotableKeyView } from "@/models/api";
import { useNotableKeys } from "./api";

const KnownKeysContext = createContext<Map<string, NotableKeyView>>(new Map());

export function KnownKeysProvider({ children }: Readonly<{ children: ReactNode }>) {
  const notable = useNotableKeys();
  const map = useMemo(() => new Map((notable.data ?? []).map((k) => [k.key_hmac, k])), [notable.data]);
  return <KnownKeysContext value={map}>{children}</KnownKeysContext>;
}

export const useKnownKey = (hmac: string): NotableKeyView | undefined => use(KnownKeysContext).get(hmac);
