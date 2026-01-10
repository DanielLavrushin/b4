import {
  Alert,
  AlertAction,
  AlertDescription,
} from "@design/components/ui/alert";
import { Button } from "@design/components/ui/button";
import { cn } from "@design/lib/utils";
import { IconX } from "@tabler/icons-react";
import {
  createContext,
  ReactNode,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";

type Severity = "error" | "warning" | "info" | "success";

interface SnackbarState {
  open: boolean;
  message: string;
  severity: Severity;
}

interface SnackbarContextType {
  showSnackbar: (message: string, severity?: Severity) => void;
  showError: (message: string) => void;
  showSuccess: (message: string) => void;
}

const SnackbarContext = createContext<SnackbarContextType | null>(null);

export function SnackbarProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<SnackbarState>({
    open: false,
    message: "",
    severity: "info",
  });

  const showSnackbar = useCallback(
    (message: string, severity: Severity = "info") => {
      setState({ open: true, message, severity });
    },
    []
  );

  const showError = useCallback(
    (message: string) => showSnackbar(message, "error"),
    [showSnackbar]
  );
  const showSuccess = useCallback(
    (message: string) => showSnackbar(message, "success"),
    [showSnackbar]
  );

  const handleClose = useCallback(() => {
    setState((prev) => ({ ...prev, open: false }));
  }, []);

  useEffect(() => {
    if (state.open) {
      const timer = setTimeout(() => {
        handleClose();
      }, 6000);
      return () => clearTimeout(timer);
    }
  }, [state.open, handleClose]);

  return (
    <SnackbarContext.Provider value={{ showSnackbar, showError, showSuccess }}>
      {children}
      <div
        className={cn(
          "fixed bottom-4 right-4 z-50 transition-all duration-300",
          state.open
            ? "translate-y-0 opacity-100"
            : "translate-y-2 opacity-0 pointer-events-none"
        )}
      >
        {state.open && (
          <Alert
            variant={state.severity === "error" ? "destructive" : "default"}
            className="min-w-75 max-w-md shadow-lg"
          >
            <AlertDescription>{state.message}</AlertDescription>
            <AlertAction>
              <Button
                variant="ghost"
                size="icon-xs"
                onClick={handleClose}
                className="h-4 w-4"
              >
                <IconX className="h-3 w-3" />
              </Button>
            </AlertAction>
          </Alert>
        )}
      </div>
    </SnackbarContext.Provider>
  );
}

export function useSnackbar(): SnackbarContextType {
  const context = useContext(SnackbarContext);
  if (!context) {
    throw new Error("useSnackbar must be used within SnackbarProvider");
  }
  return context;
}
