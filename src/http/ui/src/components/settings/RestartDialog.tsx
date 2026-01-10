import { CheckIcon, ErrorIcon, RestartIcon } from "@b4.icons";
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
import { Progress } from "@design/components/ui/progress";
import { Separator } from "@design/components/ui/separator";
import { Spinner } from "@design/components/ui/spinner";
import { useSystemRestart } from "@hooks/useSystemRestart";
import { useState } from "react";

interface RestartDialogProps {
  open: boolean;
  onClose: () => void;
}

type RestartState = "confirm" | "restarting" | "waiting" | "success" | "error";

export const RestartDialog = ({ open, onClose }: RestartDialogProps) => {
  const [state, setState] = useState<RestartState>("confirm");
  const [message, setMessage] = useState("");
  const { restart, waitForReconnection, error } = useSystemRestart();

  const handleRestart = async () => {
    setState("restarting");
    setMessage("Initiating restart...");

    const response = await restart();

    if (response?.success) {
      setState("waiting");
      setMessage("Service is restarting, waiting for reconnection...");

      const reconnected = await waitForReconnection(30);

      if (reconnected) {
        setState("success");
        setMessage("Service restarted successfully!");
        setTimeout(() => globalThis.window.location.reload(), 5000);
      } else {
        setState("error");
        setMessage("Service restart timed out. Please check manually.");
      }
    } else {
      setState("error");
      setMessage(error || "Failed to restart service");
    }
  };

  const handleClose = () => {
    if (state !== "restarting" && state !== "waiting") {
      setState("confirm");
      setMessage("");
      onClose();
    }
  };

  // Dynamic dialog props based on state
  const defaultDeailgoProps = {
    title: "Restart B4 Service",
    subtitle: "System Service Management",
    icon: <RestartIcon />,
  };

  const getDialogProps = () => {
    switch (state) {
      case "confirm":
        return {
          ...defaultDeailgoProps,
          title: "Restart B4 Service",
          subtitle: "System Service Management",
        };
      case "restarting":
      case "waiting":
        return {
          ...defaultDeailgoProps,
          title: "Restarting Service",
          subtitle: "Please wait...",
        };
      case "success":
        return {
          ...defaultDeailgoProps,
          title: "Restart Successful",
          subtitle: "Service is back online",
        };
      case "error":
        return {
          ...defaultDeailgoProps,
          title: "Restart Failed",
          subtitle: "An error occurred",
        };
      default:
        return {
          ...defaultDeailgoProps,
        };
    }
  };

  // Content for each state
  const renderContent = () => {
    switch (state) {
      case "confirm":
        return (
          <Alert>
            <AlertDescription>
              <p className="text-sm mb-2">
                This will restart the B4 service. The web interface will be
                temporarily unavailable during the restart.
              </p>
              <p className="text-xs text-muted-foreground">
                Expected downtime: 5-10 seconds
              </p>
            </AlertDescription>
          </Alert>
        );

      case "restarting":
      case "waiting":
        return (
          <div className="flex flex-col items-center gap-6 py-8">
            <div className="p-4 rounded-xl bg-accent flex items-center justify-center">
              <Spinner className="h-12 w-12" />
            </div>
            <div className="text-center">
              <h6 className="text-lg font-semibold text-foreground mb-2">
                {message}
              </h6>
              <p className="text-xs text-muted-foreground">
                Please wait, do not close this window...
              </p>
            </div>
            <div className="w-full px-4">
              <Progress />
            </div>
          </div>
        );

      case "success":
        return (
          <div className="flex flex-col items-center gap-6 py-8">
            <div className="p-4 rounded-xl bg-accent flex items-center justify-center">
              <CheckIcon className="h-16 w-16 text-secondary" />
            </div>
            <div className="text-center">
              <h6 className="text-lg font-semibold text-foreground mb-2">
                {message}
              </h6>
              <p className="text-sm text-muted-foreground">
                Reloading interface...
              </p>
            </div>
          </div>
        );

      case "error":
        return (
          <div className="flex flex-col items-center gap-6 py-8">
            <div className="p-4 rounded-xl bg-destructive/10 flex items-center justify-center">
              <ErrorIcon className="h-16 w-16 text-destructive" />
            </div>
            <div className="text-center w-full">
              <h6 className="text-lg font-semibold text-foreground mb-4">
                Restart Failed
              </h6>
              <Alert variant="destructive">
                <AlertDescription>{message}</AlertDescription>
              </Alert>
            </div>
          </div>
        );
    }
  };

  // Actions for each state
  const renderActions = () => {
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
                void handleRestart();
              }}
            >
              <RestartIcon className="h-4 w-4 mr-2" />
              Restart Service
            </Button>
          </>
        );

      case "error":
        return <Button onClick={handleClose}>Close</Button>;

      default:
        return null;
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
        <div className="py-4">{renderContent()}</div>
        {renderActions() && (
          <>
            <Separator />
            <DialogFooter>{renderActions()}</DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
};
