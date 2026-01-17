import { ControlIcon, RestartIcon, RestoreIcon } from "@b4.icons";
import { Button } from "@primitives/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@primitives/card";
import { FieldGroup } from "@primitives/field";
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
        <FieldGroup className="flex flex-col" >
          <Button
            size="lg"
            variant="outline"
            onClick={() => setShowRestartDialog(true)}
            disabled={saving}
          >
            <RestartIcon/>
            Restart B4 System Service
          </Button>
          <Button
            size="lg"
            variant="destructive"
            onClick={() => setShowResetDialog(true)}
            disabled={saving}
          >
            <RestoreIcon />
            Reset to default settings
          </Button>
        </FieldGroup>

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
