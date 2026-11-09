import { useCallback } from "react";
import {
  useSnackbar,
  type Severity,
  type SnackbarAction,
} from "@context/SnackbarProvider";
import { notifyAway, whenBack } from "@/stores/notify";

export interface Notice {
  title: string;
  body?: string;
  severity?: Severity;
  tag?: string;
  action?: SnackbarAction;
  inApp?: boolean;
}

export function useNotify() {
  const { showSnackbar } = useSnackbar();

  return useCallback(
    (notice: Notice) => {
      notifyAway({
        title: notice.title,
        body: notice.body,
        tag: notice.tag,
        onOpen: notice.action?.onClick,
      });
      if (notice.inApp === false) return;
      whenBack(() =>
        showSnackbar(
          notice.body ? `${notice.title}. ${notice.body}` : notice.title,
          notice.severity ?? "info",
          notice.action,
        ),
      );
    },
    [showSnackbar],
  );
}
