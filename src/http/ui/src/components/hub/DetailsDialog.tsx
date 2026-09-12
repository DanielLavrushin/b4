import { useMemo } from "react";
import {
  Box,
  Button,
  CircularProgress,
  DialogContent,
  Stack,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { B4Alert, B4Badge, B4Dialog, B4TextField } from "@b4.elements";
import { CommunityIcon, CopyIcon, EditIcon, ReportIcon } from "@b4.icons";
import { colors, typography } from "@design";
import { HubSet, projectionToSet } from "@models/hub";
import { copyText, describeApiError, formatBytes } from "@utils";
import { ApiError } from "@api/apiClient";
import { useSnackbar } from "@context/SnackbarProvider";
import { StrategySummary } from "@components/discovery/StrategySummary";
import { AppliedActionButton } from "./HubSetCard";
import {
  appliedAction,
  flagLabel,
  formatDate,
  reportsText,
  scorePercent,
  shortAuthor,
} from "./text";

interface DetailsDialogProps {
  open: boolean;
  set: HubSet | null;
  loading: boolean;
  error: unknown;
  busy: boolean;
  onApply: (set: HubSet) => void;
  onUpdate: (set: HubSet) => void;
  onOpenLocal: (localSetId: string) => void;
  onReport: (set: HubSet) => void;
  onClose: () => void;
}

interface FieldRowProps {
  label: string;
  children: React.ReactNode;
}

const FieldRow = ({ label, children }: FieldRowProps) => (
  <Box
    sx={{
      display: "grid",
      gridTemplateColumns: "120px 1fr",
      gap: 1.5,
      alignItems: "baseline",
    }}
  >
    <Typography
      component="span"
      sx={{
        ...typography.recipes.metricLabel,
        fontWeight: typography.weights.bold,
        color: colors.text.disabled,
      }}
    >
      {label}
    </Typography>
    <Typography
      component="div"
      variant="body2"
      sx={{ color: colors.text.primary, overflowWrap: "anywhere" }}
    >
      {children}
    </Typography>
  </Box>
);

export const DetailsDialog = ({
  open,
  set,
  loading,
  error,
  busy,
  onApply,
  onUpdate,
  onOpenLocal,
  onReport,
  onClose,
}: DetailsDialogProps) => {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const localSet = useMemo(
    () => (set ? projectionToSet(set.set, set.title) : null),
    [set],
  );
  const json = set ? JSON.stringify(set.set, null, 2) : "";
  const percent = set ? scorePercent(set.display) : null;

  const copy = async () => {
    if (await copyText(json)) showSuccess(t("core.copied"));
    else showError(t("core.copyFailed"));
  };

  return (
    <B4Dialog
      open={open}
      onClose={onClose}
      maxWidth="md"
      fullWidth
      icon={<CommunityIcon />}
      title={set?.title ?? t("hub.details.title")}
      subtitle={
        set
          ? [
              t("hub.card.by", { author: shortAuthor(set.author) }),
              t("hub.card.version", { version: set.version }),
              set.id,
            ].join(" · ")
          : undefined
      }
      actions={
        <>
          <Button onClick={onClose}>{t("core.close")}</Button>
          {set && (
            <Button
              startIcon={<ReportIcon />}
              onClick={() => onReport(set)}
              sx={{ color: colors.text.secondary }}
            >
              {t("hub.card.report")}
            </Button>
          )}
          <Box sx={{ flex: 1 }} />
          {set?.applied && appliedAction(set) === "applied" && (
            <Button
              variant="outlined"
              startIcon={<EditIcon />}
              onClick={() => onOpenLocal(set.applied?.set_id ?? "")}
            >
              {t("hub.apply.openSet")}
            </Button>
          )}
          {set && (
            <AppliedActionButton
              set={set}
              busy={busy}
              onApply={onApply}
              onUpdate={onUpdate}
            />
          )}
        </>
      }
    >
      <DialogContent sx={{ p: 0 }}>
        {loading && !set && (
          <Stack alignItems="center" sx={{ py: 4 }}>
            <CircularProgress sx={{ color: colors.secondary }} />
          </Stack>
        )}
        {!loading && !set && Boolean(error) && (
          <B4Alert
            severity={
              error instanceof ApiError && error.isNotFound ? "info" : "error"
            }
          >
            {error instanceof ApiError && error.isNotFound
              ? t("hub.details.notInCatalogue")
              : t("hub.details.loadFailed", { error: describeApiError(error) })}
          </B4Alert>
        )}
        {set && localSet && (
          <Stack spacing={2.5}>
            {set.description && (
              <Typography variant="body2" sx={{ color: colors.text.secondary }}>
                {set.description}
              </Typography>
            )}

            <Stack direction="row" spacing={0.75} useFlexGap flexWrap="wrap">
              <B4Badge
                label={t(`hub.status.set.${set.status}`, {
                  defaultValue: set.status,
                })}
                variant="outlined"
                color={set.status === "active" ? "success" : "default"}
              />
              {set.applied && (
                <B4Badge
                  label={
                    set.applied.hub_state === "modified"
                      ? t("hub.card.appliedModified")
                      : t("hub.card.applied")
                  }
                  color="success"
                  variant={
                    set.applied.hub_state === "modified" ? "outlined" : "filled"
                  }
                />
              )}
              {set.flags.map((flag) => (
                <B4Badge
                  key={flag}
                  label={flagLabel(t, flag)}
                  variant="outlined"
                  color={flag === "block" ? "error" : "default"}
                />
              ))}
            </Stack>

            <Stack spacing={0.75}>
              <FieldRow label={t("hub.details.reputation")}>
                {percent !== null
                  ? `${t("hub.card.score", { percent })} · ${reportsText(t, set.display)}`
                  : t("hub.card.reports.none")}
                {set.display.devices > 0 &&
                  ` · ${t("hub.card.devices", { count: set.display.devices })}`}
              </FieldRow>
              <FieldRow label={t("hub.details.author")}>{set.author}</FieldRow>
              <FieldRow label={t("hub.details.versions")}>
                {t("hub.details.versionLine", {
                  version: set.version,
                  min: set.b4_min,
                  built: set.b4_version || "?",
                })}
              </FieldRow>
              {(set.engine || set.family) && (
                <FieldRow label={t("hub.details.engine")}>
                  {[
                    set.engine,
                    set.family
                      ? t(`discovery.familyNames.${set.family}`, {
                          defaultValue: set.family,
                        })
                      : "",
                  ]
                    .filter(Boolean)
                    .join(" · ")}
                </FieldRow>
              )}
              <FieldRow label={t("hub.details.dates")}>
                {t("hub.details.dateLine", {
                  created: formatDate(set.created_at),
                  updated: formatDate(set.updated_at),
                })}
              </FieldRow>
              <FieldRow label={t("hub.details.fingerprint")}>
                <Box component="span" sx={typography.recipes.monoSmall}>
                  {set.fp}
                </Box>
              </FieldRow>
            </Stack>

            <Box>
              <Typography variant="subtitle2" sx={{ mb: 0.75 }}>
                {t("hub.details.targets")}
              </Typography>
              <Stack spacing={0.75}>
                <FieldRow label={t("hub.details.domains")}>
                  {set.targets.domains?.length
                    ? set.targets.domains.join(", ")
                    : t("hub.details.none")}
                </FieldRow>
                {set.targets.geosite?.length > 0 && (
                  <FieldRow label={t("hub.details.geosite")}>
                    {set.targets.geosite.join(", ")}
                  </FieldRow>
                )}
                {set.targets.geoip?.length > 0 && (
                  <FieldRow label={t("hub.details.geoip")}>
                    {set.targets.geoip.join(", ")}
                  </FieldRow>
                )}
                {set.targets.ip_count > 0 && (
                  <FieldRow label={t("hub.details.addresses")}>
                    {t("hub.card.ipCount", { count: set.targets.ip_count })}
                  </FieldRow>
                )}
              </Stack>
            </Box>

            <Box>
              <Typography variant="subtitle2" sx={{ mb: 0.75 }}>
                {t("hub.details.strategy")}
              </Typography>
              <Box
                sx={{
                  p: 1.5,
                  border: `1px solid ${colors.border.light}`,
                  borderRadius: 1.5,
                  bgcolor: colors.background.dark,
                }}
              >
                <StrategySummary
                  set={localSet}
                  domains={set.targets.domains ?? []}
                />
              </Box>
            </Box>

            <Box>
              <Typography variant="subtitle2" sx={{ mb: 0.75 }}>
                {t("hub.details.payloads")}
              </Typography>
              {set.payloads.length === 0 ? (
                <Typography variant="body2" sx={{ color: colors.text.secondary }}>
                  {t("hub.details.noPayloads")}
                </Typography>
              ) : (
                <Stack spacing={0.5}>
                  {set.payloads.map((p) => (
                    <Typography
                      key={p.sha256}
                      variant="body2"
                      sx={{
                        ...typography.recipes.monoSmall,
                        fontSize: typography.sizes.sm,
                        color: colors.text.primary,
                        overflowWrap: "anywhere",
                      }}
                    >
                      {[
                        p.protocol.toUpperCase(),
                        p.domain,
                        formatBytes(p.size),
                        p.sha256.slice(0, 16),
                      ]
                        .filter(Boolean)
                        .join(" · ")}
                    </Typography>
                  ))}
                </Stack>
              )}
            </Box>

            <Box>
              <Stack
                direction="row"
                alignItems="center"
                justifyContent="space-between"
                sx={{ mb: 0.75 }}
              >
                <Typography variant="subtitle2">
                  {t("hub.details.json")}
                </Typography>
                <Button
                  size="small"
                  startIcon={<CopyIcon />}
                  onClick={() => void copy()}
                >
                  {t("core.copy")}
                </Button>
              </Stack>
              <B4TextField
                value={json}
                selectOnFocus
                multiline
                minRows={6}
                maxRows={18}
                slotProps={{
                  input: {
                    readOnly: true,
                    sx: {
                      ...typography.recipes.monoSmall,
                      fontSize: typography.sizes.sm,
                    },
                  },
                }}
              />
            </Box>
          </Stack>
        )}
      </DialogContent>
    </B4Dialog>
  );
};
