import { useState, useEffect, forwardRef } from "react";
import {
  Button,
  Typography,
  Box,
  Divider,
  Stack,
  LinearProgress,
  Chip,
  Select,
  MenuItem,
  FormControl,
  InputLabel,
} from "@mui/material";

import {
  NewReleaseIcon,
  DescriptionIcon,
  OpenInNewIcon,
  CheckCircleIcon,
  CloseIcon,
  CloudDownloadIcon,
  InfoIcon,
  WarningIcon,
} from "@b4.icons";
import { B4Alert } from "@b4.elements";
import { B4Switch } from "@common/B4Switch";
import ReactMarkdown from "react-markdown";
import { useSystemUpdate } from "@hooks/useSystemUpdate";
import { systemApi } from "@api/settings";
import { colors } from "@design";
import { B4Dialog } from "@common/B4Dialog";
import { GitHubRelease, compareVersions } from "@hooks/useGitHubRelease";
import {
  useLocalizedChangelog,
  changelogNotesForTag,
} from "@hooks/useLocalizedChangelog";
import { useTranslation } from "react-i18next";

interface UpdateModalProps {
  open: boolean;
  onClose: () => void;
  onDismiss: () => void;
  currentVersion: string;
  releases: GitHubRelease[];
  includePrerelease: boolean;
  onTogglePrerelease: (include: boolean) => void;
}

const H2Typography = forwardRef<
  HTMLHeadingElement,
  React.ComponentProps<typeof Typography>
>(function H2Typography(props, ref) {
  return (
    <Typography
      component="h2"
      variant="subtitle2"
      sx={{ fontWeight: 800, textTransform: "uppercase" }}
      ref={ref}
      {...props}
    />
  );
});

export const UpdateModal = ({
  open,
  onClose,
  onDismiss,
  currentVersion,
  releases,
  includePrerelease,
  onTogglePrerelease,
}: UpdateModalProps) => {
  const { t, i18n } = useTranslation();
  const isLocalized = i18n.language === "ru";
  const changelogFile = isLocalized ? "changelog_ru.md" : "changelog.md";
  const localizedNotes = useLocalizedChangelog(changelogFile, isLocalized && open);
  const { performUpdate, waitForReconnection } = useSystemUpdate();
  const [updateStatus, setUpdateStatus] = useState<
    "idle" | "updating" | "reconnecting" | "success" | "error"
  >("idle");
  const [updateMessage, setUpdateMessage] = useState("");
  const [selectedVersion, setSelectedVersion] = useState<string>("");
  const [isDocker, setIsDocker] = useState(false);

  useEffect(() => {
    systemApi
      .info()
      .then((info) => {
        if (info) setIsDocker(info.is_docker);
      })
      .catch(() => {});
  }, []);

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

  const releaseNotes = selectedRelease
    ? (isLocalized &&
        changelogNotesForTag(localizedNotes, selectedRelease.tag_name)) ||
      selectedRelease.body ||
      t("update.noReleaseNotes")
    : t("update.noReleaseNotes");

  const isDowngrade =
    selectedVersion &&
    compareVersions(`v${currentVersion}`, selectedVersion) > 0;
  const isCurrent = selectedVersion === `v${currentVersion}`;

  const formatDate = (dateString: string) => {
    return new Date(dateString).toLocaleDateString(undefined, {
      year: "numeric",
      month: "long",
      day: "numeric",
    });
  };

  const handleUpdate = async () => {
    setUpdateStatus("updating");
    setUpdateMessage(t("update.initiating"));

    const result = await performUpdate(selectedVersion);
    if (!result?.success) {
      setUpdateStatus("error");
      setUpdateMessage(result?.message || t("update.failedInitiate"));
      return;
    }

    setUpdateMessage(t("update.inProgress"));
    setUpdateStatus("reconnecting");

    const reconnected = await waitForReconnection();

    if (reconnected) {
      setUpdateStatus("success");
      setUpdateMessage(t("update.completed"));
      setTimeout(() => globalThis.window.location.reload(), 5000);
    } else {
      setUpdateStatus("error");
      setUpdateMessage(t("update.manualCheck"));
    }
  };

  const isUpdating =
    updateStatus === "updating" || updateStatus === "reconnecting";

  const getDialogProps = () => {
    const base = {
      title: t("update.title"),
      subtitle: selectedRelease
        ? t("update.publishedOn", { date: formatDate(selectedRelease.published_at) })
        : "",
      icon: <NewReleaseIcon />,
    };
    switch (updateStatus) {
      case "updating":
      case "reconnecting":
        return {
          ...base,
          title: t("update.updatingTitle"),
          subtitle: t("update.pleaseWait"),
        };
      case "success":
        return { ...base, title: t("update.successTitle"), subtitle: "" };
      case "error":
        return { ...base, title: t("update.failedTitle"), subtitle: "" };
      default:
        return base;
    }
  };

  const getStatusContent = () => {
    switch (updateStatus) {
      case "updating":
      case "reconnecting":
        return (
          <Box sx={{ mb: 3 }}>
            <Typography sx={{ mb: 1, color: colors.text.secondary }}>
              {updateMessage}
            </Typography>
            <LinearProgress />
          </Box>
        );
      case "success":
        return (
          <B4Alert severity="success" icon={<CheckCircleIcon />} sx={{ mb: 2 }}>
            {updateMessage}
          </B4Alert>
        );
      case "error":
        return (
          <B4Alert severity="error" sx={{ mb: 2 }}>
            {updateMessage}
          </B4Alert>
        );
      default:
        return null;
    }
  };

  const dialogContent = () => (
    <>
      {getStatusContent()}

      {updateStatus === "idle" && (
        <Box sx={{ mb: 3 }}>
          <Stack
            direction="row"
            spacing={2}
            alignItems="center"
            sx={{ mb: 2, mt: 2 }}
          >
            <FormControl size="small" sx={{ minWidth: 220 }}>
              <InputLabel>{t("update.selectVersion")}</InputLabel>
              <Select
                value={selectedVersion}
                label={t("update.selectVersion")}
                onChange={(e) => setSelectedVersion(e.target.value)}
              >
                {releases.map((r) => (
                  <MenuItem key={r.tag_name} value={r.tag_name}>
                    {r.tag_name}
                    {r.prerelease && ` (${t("update.prerelease")})`}
                    {r.tag_name === `v${currentVersion}` && ` (${t("update.current")})`}
                  </MenuItem>
                ))}
              </Select>
            </FormControl>
            <B4Switch
              label={t("update.includePrereleases")}
              checked={includePrerelease}
              onChange={onTogglePrerelease}
            />
          </Stack>
          <Stack direction="row" spacing={1}>
            <Chip
              label={t("update.currentVersion", { version: currentVersion })}
              size="small"
              sx={{
                bgcolor: colors.accent.primary,
                color: colors.text.primary,
              }}
            />
            {!isCurrent && (
              <Chip
                label={isDowngrade ? t("update.downgrade") : t("update.upgrade")}
                size="small"
                color={isDowngrade ? "warning" : "success"}
                sx={{ fontWeight: 600 }}
              />
            )}
            {selectedRelease?.prerelease && (
              <Chip label={t("update.prerelease")} size="small" color="info" />
            )}
          </Stack>
        </Box>
      )}

      {selectedRelease && (
        <Box
          sx={{
            maxHeight: 400,
            overflow: "auto",
            p: 2,
            bgcolor: colors.background.default,
            borderRadius: 1,
            border: `1px solid ${colors.border.default}`,
          }}
        >
          <Typography
            variant="subtitle1"
            sx={{
              color: colors.secondary,
              mb: 2,
              fontWeight: 600,
              textTransform: "uppercase",
            }}
          >
            {t("update.releaseNotes", { version: selectedRelease.tag_name })}
          </Typography>
          <Box
            sx={{
              color: colors.text.primary,
              "& h1, & h2, & h3": { color: colors.secondary, mt: 2, mb: 1 },
              "& p": { mb: 1, lineHeight: 1.6 },
              "& ul, & ol": { pl: 3, mb: 1 },
              "& code": {
                bgcolor: colors.background.paper,
                color: colors.secondary,
                px: 0.5,
                py: 0.25,
                borderRadius: 0.5,
                fontSize: "0.9em",
              },
              "& a": { color: colors.secondary },
            }}
          >
            <ReactMarkdown components={{ h2: H2Typography }}>
              {releaseNotes}
            </ReactMarkdown>
          </Box>
        </Box>
      )}

      {isDocker && (
        <B4Alert severity="info" icon={<InfoIcon />} sx={{ mt: 2 }}>
          <Typography variant="body2" sx={{ fontWeight: 600, mb: 0.5 }}>
            {t("update.dockerTitle")}
          </Typography>
          <Typography variant="body2">
            {t("update.dockerDesc")}
          </Typography>
          <Box
            component="code"
            sx={{
              display: "block",
              mt: 1,
              p: 1,
              bgcolor: colors.background.default,
              borderRadius: 1,
              fontSize: "0.85em",
            }}
          >
            docker pull lavrushin/b4:latest
          </Box>
        </B4Alert>
      )}

      {updateStatus === "idle" && !isDocker && !isCurrent && (
        <B4Alert severity="warning" icon={<WarningIcon />} sx={{ mt: 2 }}>
          <Typography variant="body2">
            {t("update.backupWarning")}
          </Typography>
        </B4Alert>
      )}

      <Divider sx={{ my: 2, borderColor: colors.border.default }} />

      <Stack direction="row" spacing={2} justifyContent="center">
        <Button
          variant="outlined"
          startIcon={<DescriptionIcon />}
          href={`https://github.com/DanielLavrushin/b4/blob/main/${changelogFile}`}
          target="_blank"
          disabled={isUpdating}
        >
          {t("update.fullChangelog")}
        </Button>
        {selectedRelease && (
          <Button
            variant="outlined"
            startIcon={<OpenInNewIcon />}
            href={selectedRelease.html_url}
            target="_blank"
            disabled={isUpdating}
          >
            {t("update.viewOnGitHub")}
          </Button>
        )}
      </Stack>
    </>
  );

  const dialogActions = () => (
    <>
      <Button
        onClick={onDismiss}
        startIcon={<CloseIcon />}
        disabled={isUpdating}
      >
        {t("update.dontShowAgain")}
      </Button>
      <Box sx={{ flex: 1 }} />
      {updateStatus === "idle" && (
        <>
          <Button onClick={onClose} variant="outlined" disabled={isUpdating}>
            {t("core.close")}
          </Button>
          {!isDocker && (
            <Button
              onClick={() => void handleUpdate()}
              variant="contained"
              startIcon={<CloudDownloadIcon />}
              disabled={isUpdating || isCurrent}
              color={isDowngrade ? "warning" : "primary"}
            >
              {isDowngrade ? t("update.downgrade") : t("update.upgrade")}
            </Button>
          )}
        </>
      )}
      {updateStatus === "success" && (
        <Button
          variant="contained"
          onClick={() => globalThis.window.location.reload()}
        >
          {t("update.reloadPage")}
        </Button>
      )}
    </>
  );

  return (
    <B4Dialog
      {...getDialogProps()}
      open={open}
      onClose={isUpdating ? () => {} : onClose}
      actions={dialogActions()}
      maxWidth="lg"
    >
      {dialogContent()}
    </B4Dialog>
  );
};
