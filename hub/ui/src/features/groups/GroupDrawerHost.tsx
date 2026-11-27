import { useEffect, useState } from "react";
import { useMatch } from "react-router";
import { useModerationContext } from "@/features/moderation/ModerationProvider";
import { useOverlay } from "@/shared/hooks/useOverlay";
import { GroupDetailDrawer } from "./GroupDetailDrawer";

export const groupPath = (id: string, version?: number) =>
  version ? `/groups/${encodeURIComponent(id)}/v/${String(version)}` : `/groups/${encodeURIComponent(id)}`;

export function GroupDrawerHost() {
  const plain = useMatch("/groups/:id");
  const versioned = useMatch("/groups/:id/v/:version");
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

  return <GroupDetailDrawer id={shown} open={id !== null} focusVersion={version} onClose={() => overlay.close("/queue")} moderation={moderation} />;
}
