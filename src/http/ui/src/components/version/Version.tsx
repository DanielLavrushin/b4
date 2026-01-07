import { dismissVersion, useGitHubRelease } from "@hooks/useGitHubRelease";
import { useState } from "react";
import { VersionBadge } from "./Badge";
import { UpdateModal } from "./UpdateDialog";

export default function Version() {
  const [updateModalOpen, setUpdateModalOpen] = useState(false);
  const {
    releases,
    latestRelease,
    isNewVersionAvailable,
    isLoading,
    currentVersion,
    includePrerelease,
    setIncludePrerelease,
  } = useGitHubRelease();

  const handleVersionClick = () => {
    setUpdateModalOpen(true);
  };

  const handleDismissUpdate = () => {
    if (latestRelease) {
      dismissVersion(latestRelease.tag_name);
    }
    setUpdateModalOpen(false);
  };

  return (
    <>
      <VersionBadge
        version={currentVersion}
        hasUpdate={isNewVersionAvailable}
        isLoading={isLoading}
        onClick={handleVersionClick}
      />

      <UpdateModal
        open={updateModalOpen}
        onClose={() => setUpdateModalOpen(false)}
        onDismiss={handleDismissUpdate}
        currentVersion={currentVersion}
        releases={releases}
        includePrerelease={includePrerelease}
        onTogglePrerelease={setIncludePrerelease}
      />
    </>
  );
}
