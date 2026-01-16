import { ControlIcon, RestartIcon, RestoreIcon } from "@b4.icons";
import { Button } from "@primitives/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@primitives/card";
import { useState } from "react";
import { ResetDialog } from "./ResetDialog";
import { RestartDialog } from "./RestartDialog";

interface ControlSettingsProps {
  loadConfig: () => void;
}

export const ControlSettings = ({ loadConfig }: ControlSettingsProps) => {
  const [saving] = useState(false);
  const [showRestartDialog, setShowRestartDialog] = useState(false);
  const [showResetDialog, setShowResetDialog] = useState(false);

  const handleResetSuccess = () => {
    loadConfig();
  };

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <ControlIcon />
          <CardTitle>Core Controls</CardTitle>
        </div>
        <CardDescription>
          Control core service and config operations
        </CardDescription>
      </CardHeader>
      <CardContent>
        <div className="flex flex-col gap-4">
          <Button
            size="sm"
            variant="outline"
            onClick={() => setShowRestartDialog(true)}
            disabled={saving}
          >
            <RestartIcon className="mr-2 size-4" />
            Restart B4 System Service
          </Button>
          <Button
            size="sm"
            variant="destructive"
            onClick={() => setShowResetDialog(true)}
            disabled={saving}
          >
            <RestoreIcon className="mr-2 size-4" />
            Reset the configuration to default settings
          </Button>
        </div>

        <RestartDialog
          open={showRestartDialog}
          onClose={() => setShowRestartDialog(false)}
        />

        <ResetDialog
          open={showResetDialog}
          onClose={() => setShowResetDialog(false)}
          onSuccess={handleResetSuccess}
        />
      </CardContent>
    </Card>
  );
};
