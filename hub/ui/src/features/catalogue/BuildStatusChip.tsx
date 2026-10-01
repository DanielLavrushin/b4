import { Box, Chip, CircularProgress, IconButton, Tooltip } from "@mui/material";
import CheckIcon from "@mui/icons-material/CheckCircleOutline";
import ErrorIcon from "@mui/icons-material/ErrorOutline";
import ScheduleIcon from "@mui/icons-material/Schedule";
import ReplayIcon from "@mui/icons-material/Replay";
import { useNavigate } from "react-router";
import { useTranslation } from "react-i18next";
import { useSnackbar } from "@/app/SnackbarProvider";
import { formatAgo, formatStamp } from "@/shared/utils/format";
import { useBuildStatus, useCatalogueBuild, useRefreshOnPublish } from "./api";

export function BuildStatusChip() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const status = useBuildStatus();
  useRefreshOnPublish(status.data);
  const build = useCatalogueBuild();
  const { notifyResult, notifyError } = useSnackbar();
  const data = status.data;
  if (!data) return null;

  const retry = async () => {
    try {
      notifyResult(await build.mutateAsync());
    } catch (err) {
      notifyError(err);
    }
  };

  const busy = data.state === "queued" || data.state === "building";
  const failed = !busy && data.last_error !== undefined;
  const lastOk = data.last_ok?.finished_at;
  let label: string;
  let tooltip: string;
  let color: "default" | "success" | "warning" | "error" | "info" = "default";
  let icon = <CheckIcon fontSize="small" />;
  if (busy) {
    label = t(`build.${data.state}`);
    tooltip = t("build.busyHint", { trigger: t(`build.trigger.${data.trigger ?? "manual"}`, { defaultValue: data.trigger }) });
    color = "info";
    icon = <CircularProgress size={12} color="inherit" />;
  } else if (failed) {
    label = t("build.failed");
    tooltip = t("build.failedHint", { when: formatStamp(data.last_error?.finished_at), message: data.last_error?.error ?? "" });
    color = "error";
    icon = <ErrorIcon fontSize="small" />;
  } else if (data.dirty) {
    label = t("build.dirty");
    tooltip = t("build.dirtyHint", { when: formatAgo(t, lastOk) });
    color = "warning";
    icon = <ScheduleIcon fontSize="small" />;
  } else {
    label = t("build.published", { when: formatAgo(t, lastOk) });
    tooltip = data.last_ok ? t("build.publishedHint", { file: data.last_ok.file ?? "", sets: data.last_ok.sets, when: formatStamp(lastOk) }) : t("build.never");
    color = "success";
  }

  return (
    <Box sx={{ display: "flex", alignItems: "center", gap: 0.5, mr: 1 }}>
      <Tooltip title={tooltip}>
        <Chip
          size="small"
          variant="outlined"
          color={color}
          icon={icon}
          label={label}
          onClick={() => void navigate("/catalogue")}
          sx={{ color: "#fff", "& .MuiChip-icon": { color: "inherit" }, maxWidth: { xs: 140, sm: 260 } }}
        />
      </Tooltip>
      {failed && (
        <Tooltip title={t("build.retry")}>
          <span>
            <IconButton size="small" color="inherit" disabled={build.isPending} onClick={() => void retry()}>
              <ReplayIcon fontSize="small" />
            </IconButton>
          </span>
        </Tooltip>
      )}
    </Box>
  );
}
