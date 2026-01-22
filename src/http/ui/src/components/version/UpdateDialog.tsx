import { forwardRef, useEffect, useState } from "react";

import {
  CheckCircleIcon,
  CloseIcon,
  CloudDownloadIcon,
  DescriptionIcon,
  NewReleaseIcon,
  OpenInNewIcon,
} from "@b4.icons";
import { Alert, AlertDescription } from "@primitives/alert";
import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import { Card, CardContent, CardHeader, CardTitle } from "@primitives/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@primitives/dialog";
import { ItemActions, ItemContent, ItemGroup } from "@primitives/item";
import { Label } from "@primitives/label";
import { Progress } from "@primitives/progress";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@primitives/select";
import { Separator } from "@primitives/separator";
import { Switch } from "@primitives/switch";
import { cn } from "@design/lib/utils";
import { GitHubRelease, compareVersions } from "@hooks/useGitHubRelease";
import { useSystemUpdate } from "@hooks/useSystemUpdate";
import React from "react";
import ReactMarkdown from "react-markdown";

interface UpdateModalProps {
  open: boolean;
  onClose: () => void;
  onDismiss: () => void;
  currentVersion: string;
  releases: GitHubRelease[];
  includePrerelease: boolean;
  onTogglePrerelease: (include: boolean) => void;
}

const H2Typography = forwardRef<HTMLHeadingElement, React.ComponentProps<"h2">>(
  function H2Typography(props, ref) {
    return <CardTitle ref={ref} {...props} />;
  },
);

export const UpdateModal = ({
  open,
  onClose,
  onDismiss,
  currentVersion,
  releases,
  includePrerelease,
  onTogglePrerelease,
}: UpdateModalProps) => {
  const { performUpdate, waitForReconnection } = useSystemUpdate();
  const [updateStatus, setUpdateStatus] = useState<
    "idle" | "updating" | "reconnecting" | "success" | "error"
  >("idle");
  const [updateMessage, setUpdateMessage] = useState("");
  const [selectedVersion, setSelectedVersion] = useState<string>("");

  useEffect(() => {
    if (releases.length > 0 && !selectedVersion) {
      setSelectedVersion(releases[0].tag_name);
    }
  }, [releases, selectedVersion]);

  useEffect(() => {
    if (!open) {
      setUpdateStatus("idle");
      setUpdateMessage("");
    }
  }, [open]);

  const selectedRelease =
    releases.find((r) => r.tag_name === selectedVersion) || releases[0];

  const isDowngrade =
    selectedVersion &&
    compareVersions(`v${currentVersion}`, selectedVersion) > 0;
  const isCurrent = selectedVersion === `v${currentVersion}`;

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString("en-US", {
      year: "numeric",
      month: "long",
      day: "numeric",
    });
  };

  const handleUpdate = async () => {
    setUpdateStatus("updating");
    setUpdateMessage("Initiating update...");

    const result = await performUpdate(selectedVersion);
    if (!result || !result.success) {
      setUpdateStatus("error");
      setUpdateMessage(result?.message || "Failed to initiate update.");
      return;
    }

    setUpdateMessage("Update in progress. Waiting for service to restart...");
    setUpdateStatus("reconnecting");

    const reconnected = await waitForReconnection();

    if (reconnected) {
      setUpdateStatus("success");
      setUpdateMessage("Update completed successfully! Refreshing...");
      setTimeout(() => globalThis.window.location.reload(), 5000);
    } else {
      setUpdateStatus("error");
      setUpdateMessage(
        "Update may have completed but service did not restart. Please check manually.",
      );
    }
  };

  const isUpdating =
    updateStatus === "updating" || updateStatus === "reconnecting";

  const getDialogProps = () => {
    const base = {
      title: "Version Management",
      subtitle: selectedRelease
        ? `Published on ${formatDate(selectedRelease.published_at)}`
        : "",
      icon: <NewReleaseIcon />,
    };
    switch (updateStatus) {
      case "updating":
      case "reconnecting":
        return {
          ...base,
          title: "Updating B4 Service",
          subtitle: "Please wait...",
        };
      case "success":
        return { ...base, title: "Update Successful", subtitle: "" };
      case "error":
        return { ...base, title: "Update Failed", subtitle: "" };
      default:
        return base;
    }
  };

  const getStatusContent = () => {
    switch (updateStatus) {
      case "updating":
      case "reconnecting":
        return (
          <ItemGroup>
            <ItemContent>
              <p className="text-muted-foreground">{updateMessage}</p>
              <Progress />
            </ItemContent>
          </ItemGroup>
        );
      case "success":
        return (
          <Alert>
            <CheckCircleIcon />
            <AlertDescription>{updateMessage}</AlertDescription>
          </Alert>
        );
      case "error":
        return (
          <Alert variant="destructive">
            <AlertDescription>{updateMessage}</AlertDescription>
          </Alert>
        );
      default:
        return null;
    }
  };

  const dialogContent = () => (
    <>
      {getStatusContent()}

      {updateStatus === "idle" && (
        <ItemGroup>
          <ItemGroup>
            <ItemContent>
              <Label htmlFor="select-version">Select Version</Label>
              <Select
                value={selectedVersion}
                onValueChange={(value) => setSelectedVersion(value)}
              >
                <SelectTrigger id="select-version">
                  <SelectValue placeholder="Select Version" />
                </SelectTrigger>
                <SelectContent>
                  {releases.map((r) => (
                    <SelectItem key={r.tag_name} value={r.tag_name}>
                      {r.tag_name}
                      {r.prerelease && " (pre-release)"}
                      {r.tag_name === `v${currentVersion}` && " (current)"}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </ItemContent>
            <ItemActions>
              <Switch
                checked={includePrerelease}
                onCheckedChange={(checked) => onTogglePrerelease(checked)}
              />
              <Label>Include pre-releases</Label>
            </ItemActions>
          </ItemGroup>
          <ItemActions>
            <Badge>{`Current: v${currentVersion}`}</Badge>

            {selectedRelease?.prerelease && (
              <Badge variant="outline">Pre-release</Badge>
            )}
          </ItemActions>
        </ItemGroup>
      )}
      <Separator />
      {selectedRelease && (
        <Card>
          <CardContent>
            <ReactMarkdown components={{ h2: H2Typography }}>
              {selectedRelease.body || "No release notes available."}
            </ReactMarkdown>
          </CardContent>
        </Card>
      )}
      <Separator />
      <ItemGroup>
        <Button variant="outline" asChild>
          <a
            href="https://github.com/DanielLavrushin/b4/blob/main/changelog.md"
            target="_blank"
            rel="noopener noreferrer"
          >
            <DescriptionIcon />
            Full Changelog
          </a>
        </Button>
        {selectedRelease && (
          <Button variant="outline" asChild>
            <a
              href={selectedRelease.html_url}
              target="_blank"
              rel="noopener noreferrer"
            >
              <OpenInNewIcon />
              View on GitHub
            </a>
          </Button>
        )}
      </ItemGroup>
    </>
  );

  const dialogActions = () => (
    <>
      <Button onClick={onDismiss} variant="ghost" disabled={isUpdating}>
        <CloseIcon />
        Don't Show Again
      </Button>
      <div className="flex-1" />
      {updateStatus === "idle" && (
        <>
          <Button onClick={onClose} variant="outline" disabled={isUpdating}>
            Close
          </Button>
          <Button
            onClick={() => void handleUpdate()}
            disabled={isUpdating || isCurrent}
            className={cn(
              isDowngrade && "bg-destructive hover:bg-destructive/90",
            )}
          >
            <CloudDownloadIcon className="mr-2 size-4" />
            {isDowngrade ? "Downgrade" : "Update"}
          </Button>
        </>
      )}
      {updateStatus === "success" && (
        <Button onClick={() => globalThis.window.location.reload()}>
          Reload Page
        </Button>
      )}
    </>
  );

  const dialogProps = getDialogProps();

  return (
    <Dialog
      open={open}
      onOpenChange={(open) => !open && (isUpdating ? () => {} : onClose())}
    >
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader icon={dialogProps.icon}>
          <DialogTitle>{dialogProps.title}</DialogTitle>
          {dialogProps.subtitle && (
            <DialogDescription className="mt-1">
              {dialogProps.subtitle}
            </DialogDescription>
          )}
        </DialogHeader>
        {dialogContent()}
        {dialogActions() && (
          <>
            <DialogFooter>{dialogActions()}</DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
};
