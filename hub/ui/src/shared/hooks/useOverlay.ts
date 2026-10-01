import { useCallback } from "react";
import { useLocation, useNavigate, type Location } from "react-router";

interface OverlayState {
  background?: Location;
}

export const backgroundOf = (location: Location): Location | undefined => (location.state as OverlayState | null)?.background;

export function useOverlay() {
  const navigate = useNavigate();
  const location = useLocation();
  const background = backgroundOf(location);

  const open = useCallback(
    (to: string) => {
      void navigate(to, { state: { background: background ?? location } });
    },
    [navigate, background, location],
  );

  const close = useCallback(
    (fallback: string) => {
      if (background && location.key !== "default") {
        void navigate(-1);
        return;
      }
      void navigate(fallback, { replace: true });
    },
    [navigate, background, location.key],
  );

  return { open, close, background };
}
