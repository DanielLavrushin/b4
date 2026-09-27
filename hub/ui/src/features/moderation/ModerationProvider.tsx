import { createContext, use, type ReactNode } from "react";
import { useModeration, type Moderation } from "./useModeration";

const ModerationContext = createContext<Moderation | null>(null);

export function ModerationProvider({ children }: Readonly<{ children: ReactNode }>) {
  const moderation = useModeration();
  return (
    <ModerationContext value={moderation}>
      {children}
      {moderation.dialog}
    </ModerationContext>
  );
}

export const useModerationContext = (): Moderation => {
  const ctx = use(ModerationContext);
  if (!ctx) throw new Error("useModerationContext outside ModerationProvider");
  return ctx;
};
