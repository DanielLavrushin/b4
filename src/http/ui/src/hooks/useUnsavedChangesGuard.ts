import { useEffect } from "react";
import { useBlocker, type BlockerFunction } from "react-router";

export function useUnsavedChangesGuard(
  shouldBlock: boolean | BlockerFunction,
  unloadWhen: boolean,
) {
  const blocker = useBlocker(shouldBlock);

  useEffect(() => {
    if (!unloadWhen) return;
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault();
    };
    window.addEventListener("beforeunload", onBeforeUnload);
    return () => window.removeEventListener("beforeunload", onBeforeUnload);
  }, [unloadWhen]);

  return blocker;
}
