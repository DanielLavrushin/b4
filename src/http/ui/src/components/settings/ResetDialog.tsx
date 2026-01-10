import { CheckIcon, ErrorIcon, RestoreIcon, SecurityIcon } from "@b4.icons";
import { Alert, AlertDescription } from "@design/components/ui/alert";
import { Button } from "@design/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@design/components/ui/dialog";
import { Separator } from "@design/components/ui/separator";
import { Spinner } from "@design/components/ui/spinner";
import { useConfigReset } from "@hooks/useConfig";
import { useState } from "react";

interface ResetDialogProps {
  open: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

type ResetState = "confirm" | "resetting" | "success" | "error";

export const ResetDialog = ({ open, onClose, onSuccess }: ResetDialogProps) => {
  const [state, setState] = useState<ResetState>("confirm");
  const [message, setMessage] = useState("");
  const { resetConfig } = useConfigReset();

  const handleReset = async () => {
    setState("resetting");
    setMessage("Resetting configuration...");

    const response = await resetConfig();

    if (response?.success) {
      setState("success");
      setMessage("Configuration reset successfully!");
      setTimeout(() => {
        handleClose();
        onSuccess();
      }, 2000);
    } else {
      setState("error");
      setMessage("Failed to reset configuration");
    }
  };

  const handleClose = () => {
    if (state !== "resetting") {
      setState("confirm");
      setMessage("");
      onClose();
    }
  };

  const defaultProps = {
    title: "Reset Configuration",
    subtitle: "Restore default settings",
    icon: <RestoreIcon />,
  };

  // Dynamic dialog props based on state
  const getDialogProps = () => {
    switch (state) {
      case "confirm":
        return {
          ...defaultProps,
          title: "Restart B4 Service",
          subtitle: "System Service Management",
        };
      case "resetting":
        return {
          ...defaultProps,
          title: "Resetting Configuration",
          subtitle: "Please wait...",
          icon: <Spinner className="h-4 w-4" />,
        };
      case "success":
        return {
          ...defaultProps,
          title: "Restart Successful",
          subtitle: "Service is back online",
        };
      case "error":
        return {
          ...defaultProps,
          title: "Restart Failed",
          subtitle: "An error occurred",
          icon: <ErrorIcon />,
        };
      default:
        return {
          ...defaultProps,
        };
    }
  };

  const getDialogActions = () => {
    switch (state) {
      case "confirm":
        return (
          <>
            <Button onClick={handleClose} variant="ghost">
              Cancel
            </Button>
            <div className="flex-1" />
            <Button
              onClick={() => {
                void handleReset();
              }}
            >
              <RestoreIcon className="h-4 w-4 mr-2" />
              Reset to Defaults
            </Button>
          </>
        );
      case "error":
        return (
          <Button onClick={handleClose}>
            Close
          </Button>
        );

      case "success":
      default:
        return null;
    }
  };

  const getDialogContent = () => {
    switch (state) {
      case "confirm":
        return (
          <>
            <Alert>
              <AlertDescription>
                Network, DPI bypass, protocol, and logging settings will be
                reset to defaults. You may need to restart B4 for some changes
                to take effect.
              </AlertDescription>
            </Alert>
            <Alert variant="destructive">
              <AlertDescription>
                This will reset all configuration to default values except:
              </AlertDescription>
            </Alert>
            <ul className="space-y-2 mt-4">
              <li className="flex items-start gap-3">
                <SecurityIcon className="h-5 w-5 text-secondary mt-0.5 shrink-0" />
                <div>
                  <p className="text-sm font-medium">Domain Configuration</p>
                  <p className="text-xs text-muted-foreground">
                    All domain filters and geodata settings will be preserved
                  </p>
                </div>
              </li>
              <li className="flex items-start gap-3">
                <SecurityIcon className="h-5 w-5 text-secondary mt-0.5 shrink-0" />
                <div>
                  <p className="text-sm font-medium">Testing Configuration</p>
                  <p className="text-xs text-muted-foreground">
                    Checker settings and test domains will be preserved
                  </p>
                </div>
              </li>
            </ul>
          </>
        );

      case "resetting":
        return (
          <div className="flex flex-col items-center gap-6 py-8">
            <Spinner className="h-12 w-12" />
            <h6 className="text-lg font-semibold text-foreground">{message}</h6>
          </div>
        );

      case "success":
        return (
          <div className="flex flex-col items-center gap-6 py-8">
            <CheckIcon className="h-16 w-16 text-secondary" />
            <h6 className="text-lg font-semibold text-foreground">{message}</h6>
          </div>
        );

      case "error":
        return (
          <div className="flex flex-col items-center gap-6 py-8">
            <ErrorIcon className="h-16 w-16 text-destructive" />
            <Alert variant="destructive">
              <AlertDescription>{message}</AlertDescription>
            </Alert>
          </div>
        );
    }
  };

  const dialogProps = getDialogProps();

  return (
    <Dialog open={open} onOpenChange={(open) => !open && handleClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
              {dialogProps.icon}
            </div>
            <div className="flex-1">
              <DialogTitle>{dialogProps.title}</DialogTitle>
              <DialogDescription className="mt-1">
                {dialogProps.subtitle}
              </DialogDescription>
            </div>
          </div>
        </DialogHeader>
        <div className="py-4">{getDialogContent()}</div>
        {getDialogActions() && (
          <>
            <Separator />
            <DialogFooter>{getDialogActions()}</DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
};
