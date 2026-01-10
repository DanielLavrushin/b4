import { ControlIcon, RestartIcon, RestoreIcon } from "@b4.icons";
import { Button } from "@design/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@design/components/ui/card";
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
          <ControlIcon className="h-5 w-5" />
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
            <RestartIcon className="h-4 w-4 mr-2" />
            Restart B4 System Service
          </Button>
          <Button
            size="sm"
            variant="destructive"
            onClick={() => setShowResetDialog(true)}
            disabled={saving}
          >
            <RestoreIcon className="h-4 w-4 mr-2" />
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
