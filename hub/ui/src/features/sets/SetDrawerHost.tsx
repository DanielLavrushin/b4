import { useEffect, useState } from "react";
import { useMatch } from "react-router";
import { useModerationContext } from "@/features/moderation/ModerationProvider";
import { useOverlay } from "@/shared/hooks/useOverlay";
import { SetDetailDrawer } from "./SetDetailDrawer";

export const setPath = (id: string, version?: number) =>
  version ? `/sets/${encodeURIComponent(id)}/v/${String(version)}` : `/sets/${encodeURIComponent(id)}`;

export function SetDrawerHost() {
  const plain = useMatch("/sets/:id");
  const versioned = useMatch("/sets/:id/v/:version");
  const match = versioned ?? plain;
  const id = match?.params.id ?? null;
  const version = versioned ? Number(versioned.params.version) : undefined;
  const [shown, setShown] = useState<string | null>(id);
  const overlay = useOverlay();
  const moderation = useModerationContext();

  useEffect(() => {
    if (id) {
      setShown(id);
      return;
    }
    const handle = setTimeout(() => setShown(null), 400);
    return () => clearTimeout(handle);
  }, [id]);

  return <SetDetailDrawer id={shown} open={id !== null} focusVersion={version} onClose={() => overlay.close("/sets")} moderation={moderation} />;
}
