import { useEffect, useState } from "react";
import { useMatch } from "react-router";
import { useOverlay } from "@/shared/hooks/useOverlay";
import { KeyDetailDrawer } from "./KeyDetailDrawer";

export function KeyDrawerHost() {
  const match = useMatch("/keys/:hmac");
  const key = match?.params.hmac ?? null;
  const [shown, setShown] = useState<string | null>(key);
  const overlay = useOverlay();

  useEffect(() => {
    if (key) setShown(key);
  }, [key]);

  return <KeyDetailDrawer keyHmac={shown} open={key !== null} onClose={() => overlay.close("/keys")} />;
}
