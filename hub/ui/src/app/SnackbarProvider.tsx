import {
  createContext,
  use,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { Alert, Snackbar } from "@mui/material";
import { useTranslation } from "react-i18next";
import { errorText, noticeText, type Coded } from "@/shared/utils/notices";

type Severity = "error" | "warning" | "info" | "success";

interface Item {
  key: number;
  message: string;
  severity: Severity;
}

interface SnackbarContextValue {
  notify: (message: string, severity?: Severity) => void;
  notifyError: (error: unknown) => void;
  notifyResult: (result: Coded) => void;
}

const SnackbarContext = createContext<SnackbarContextValue | null>(null);

export function SnackbarProvider({ children }: Readonly<{ children: ReactNode }>) {
  const { t } = useTranslation();
  const [queue, setQueue] = useState<Item[]>([]);
  const [open, setOpen] = useState(false);
  const keyRef = useRef(0);
  const current = queue[0];

  const notify = useCallback((message: string, severity: Severity = "info") => {
    keyRef.current += 1;
    setQueue((q) => [...q, { key: keyRef.current, message, severity }]);
  }, []);

  const notifyError = useCallback(
    (error: unknown) => {
      notify(t("app.error", { message: errorText(t, error) }), "error");
    },
    [notify, t],
  );

  const notifyResult = useCallback(
    (result: Coded) => {
      notify(noticeText(t, result), "success");
    },
    [notify, t],
  );

  useEffect(() => {
    if (current) setOpen(true);
  }, [current]);

  const value = useMemo(() => ({ notify, notifyError, notifyResult }), [notify, notifyError, notifyResult]);

  return (
    <SnackbarContext value={value}>
      {children}
      <Snackbar
        key={current?.key}
        open={open && current !== undefined}
        autoHideDuration={4000}
        onClose={(_e, reason) => {
          if (reason !== "clickaway") setOpen(false);
        }}
        slotProps={{ transition: { onExited: () => setQueue((q) => q.slice(1)) } }}
        anchorOrigin={{ vertical: "bottom", horizontal: "center" }}
      >
        <Alert
          severity={current?.severity ?? "info"}
          variant="filled"
          onClose={() => setOpen(false)}
        >
          {current?.message}
        </Alert>
      </Snackbar>
    </SnackbarContext>
  );
}

export const useSnackbar = (): SnackbarContextValue => {
  const ctx = use(SnackbarContext);
  if (!ctx) throw new Error("useSnackbar outside SnackbarProvider");
  return ctx;
};
