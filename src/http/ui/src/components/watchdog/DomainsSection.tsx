import { useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { Link as RouterLink } from "react-router";
import {
  Button,
  IconButton,
  Link,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  Tooltip,
  Typography,
} from "@mui/material";
import {
  DeleteIcon,
  DomainIcon,
  ErrorIcon,
  StartIcon,
  SuccessIcon,
  SwapIcon,
  TimerIcon,
  WarningIcon,
} from "@b4.icons";
import { colors } from "@design";
import {
  B4Alert,
  B4Badge,
  B4PlusButton,
  B4Section,
  B4TextField,
} from "@b4.elements";
import { useSnackbar } from "@context/SnackbarProvider";
import { B4SetConfig } from "@models/config";
import { WatchdogDomainStatus, setWatchBlock } from "@models/watchdog";
import { MAX_PROBE_URLS, describeCodedError, normalizeProbeUrl } from "@utils";
import { fullTime, timeAgo } from "./format";

function statusColor(
  status: string,
): "primary" | "secondary" | "error" | "info" {
  switch (status) {
    case "healthy":
      return "primary";
    case "degraded":
      return "secondary";
    default:
      return "info";
  }
}

function StatusIcon({ status }: Readonly<{ status: string }>) {
  switch (status) {
    case "healthy":
      return <SuccessIcon sx={{ fontSize: 18, color: colors.primary }} />;
    case "degraded":
      return <WarningIcon sx={{ fontSize: 18, color: colors.secondary }} />;
    case "escalating":
    case "queued":
      return <TimerIcon sx={{ fontSize: 18, color: colors.state.info }} />;
    default:
      return <ErrorIcon sx={{ fontSize: 18, color: colors.state.error }} />;
  }
}

function moveBlockFor(
  domain: WatchdogDomainStatus,
  owner: B4SetConfig | undefined,
): string | null {
  if (!owner) return null;
  const block = setWatchBlock({
    ...owner,
    discovery: { urls: ["https://" + (domain.display_domain || domain.domain) + "/"] },
  });
  if (block) return block;
  const urls = owner.discovery?.urls ?? [];
  const host = normalizeProbeUrl(domain.display_domain || domain.domain)?.host;
  const known = urls.some((u) => normalizeProbeUrl(u)?.host === host);
  if (!known && urls.length >= MAX_PROBE_URLS) return "too_many_urls";
  return null;
}

function DomainRow({
  domain,
  enabled,
  moving,
  owner,
  onForceCheck,
  onRemove,
  onMove,
}: Readonly<{
  domain: WatchdogDomainStatus;
  enabled: boolean;
  moving: boolean;
  owner?: B4SetConfig;
  onForceCheck: (d: string) => void;
  onRemove: (d: string) => void;
  onMove: (d: WatchdogDomainStatus) => void;
}>) {
  const { t } = useTranslation();
  const isEscalating = domain.status === "escalating";
  const ownerName = domain.owner_set_name || domain.owner_set_id;
  const watchedBy = domain.watched_by_set_name || domain.watched_by_set_id;
  const moveBlock = moveBlockFor(domain, owner);

  return (
    <TableRow
      sx={{
        "&:last-child td, &:last-child th": { border: 0 },
        opacity: isEscalating ? 0.8 : 1,
      }}
    >
      <TableCell>
        <Tooltip title={domain.domain === domain.display_domain ? "" : domain.domain}>
          <Stack direction="row" spacing={1} alignItems="center">
            <StatusIcon status={domain.status} />
            <Typography variant="body2" sx={{ fontWeight: 500 }}>
              {domain.display_domain || domain.domain}
            </Typography>
          </Stack>
        </Tooltip>
      </TableCell>
      <TableCell>
        {domain.matched_set ? (
          <B4Badge label={domain.matched_set} variant="outlined" />
        ) : (
          <Typography variant="body2" color="text.secondary">-</Typography>
        )}
      </TableCell>
      <TableCell>
        {watchedBy ? (
          <B4Badge
            label={t("watchdog.domains.watchedBy", { name: watchedBy })}
            color="info"
            variant="outlined"
          />
        ) : (
          <B4Badge
            label={t(`watchdog.status.${domain.status}`)}
            color={statusColor(domain.status)}
            variant="outlined"
          />
        )}
      </TableCell>
      <TableCell>
        <Tooltip title={fullTime(domain.last_check)}>
          <Typography variant="body2" color="text.secondary">
            {timeAgo(t, domain.last_check)}
          </Typography>
        </Tooltip>
      </TableCell>
      <TableCell>
        {domain.consecutive_failures > 0 ? (
          <B4Badge
            label={`${domain.consecutive_failures}`}
            color="secondary"
            variant="outlined"
          />
        ) : (
          <Typography variant="body2" color="text.secondary">
            0
          </Typography>
        )}
      </TableCell>
      <TableCell>
        {domain.last_error ? (
          <Tooltip title={domain.last_error}>
            <B4Badge
              label={domain.last_error}
              color="error"
              variant="outlined"
              sx={{ maxWidth: 250 }}
            />
          </Tooltip>
        ) : (
          <Typography variant="body2" color="text.secondary">-</Typography>
        )}
      </TableCell>
      <TableCell align="right">
        <Stack
          direction="row"
          spacing={0.5}
          justifyContent="flex-end"
          alignItems="center"
        >
          {domain.owner_set_id && !watchedBy && (
            <Tooltip
              title={
                moveBlock
                  ? t(`watchdog.errors.${moveBlock}`)
                  : t("watchdog.domains.moveHint", { name: ownerName })
              }
            >
              <span>
                <Button
                  size="small"
                  startIcon={<SwapIcon sx={{ fontSize: 18 }} />}
                  onClick={() => onMove(domain)}
                  disabled={moving || isEscalating || !!moveBlock}
                  sx={{
                    textTransform: "none",
                    maxWidth: 220,
                    whiteSpace: "nowrap",
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                    display: "inline-flex",
                  }}
                >
                  {t("watchdog.domains.moveTo", { name: ownerName })}
                </Button>
              </span>
            </Tooltip>
          )}
          <Tooltip title={enabled ? t("watchdog.forceCheck") : t("watchdog.forceCheckDisabled")}>
            <span>
              <IconButton
                size="small"
                onClick={() => onForceCheck(domain.domain)}
                disabled={isEscalating || !enabled}
              >
                <StartIcon sx={{ fontSize: 18 }} />
              </IconButton>
            </span>
          </Tooltip>
          <Tooltip title={t("watchdog.removeDomain")}>
            <IconButton
              size="small"
              onClick={() => onRemove(domain.domain)}
              color="error"
            >
              <DeleteIcon sx={{ fontSize: 18 }} />
            </IconButton>
          </Tooltip>
        </Stack>
      </TableCell>
    </TableRow>
  );
}

interface DomainsSectionProps {
  domains: WatchdogDomainStatus[];
  enabled: boolean;
  setConfigs: Map<string, B4SetConfig>;
  onForceCheck: (domain: string) => void;
  onRemove: (domain: string) => void;
  onAdd: (domain: string) => Promise<void>;
  onMove: (domain: string, setId: string) => Promise<void>;
}

export function DomainsSection({
  domains,
  enabled,
  setConfigs,
  onForceCheck,
  onRemove,
  onAdd,
  onMove,
}: Readonly<DomainsSectionProps>) {
  const { t } = useTranslation();
  const { showError, showSuccess } = useSnackbar();
  const [newDomain, setNewDomain] = useState("");
  const [moving, setMoving] = useState<string | null>(null);

  const healthyCount = domains.filter((d) => d.status === "healthy").length;
  const queuedCount = domains.filter((d) => d.status === "queued").length;
  const degradedCount = domains.filter(
    (d) => d.status !== "healthy" && d.status !== "queued",
  ).length;

  const handleAddDomain = () => {
    const domain = newDomain.trim();
    if (!domain) return;
    onAdd(domain)
      .then(() => setNewDomain(""))
      .catch(() => {});
  };

  const handleMove = (entry: WatchdogDomainStatus) => {
    const setId = entry.owner_set_id;
    if (!setId) return;
    const name = entry.owner_set_name || setId;
    const label = entry.display_domain || entry.domain;
    setMoving(entry.domain);
    onMove(entry.domain, setId)
      .then(() =>
        showSuccess(t("watchdog.domains.moved", { domain: label, name })),
      )
      .catch((e: unknown) =>
        showError(
          t("watchdog.domains.moveFailed", {
            domain: label,
            error: describeCodedError(e, t, "watchdog.errors"),
          }),
        ),
      )
      .finally(() => setMoving(null));
  };

  return (
    <B4Section
      title={t("watchdog.domains.title")}
      description={t("watchdog.domains.description")}
      icon={<DomainIcon />}
    >
      <Stack spacing={2}>
        <Typography variant="body2" color="text.secondary">
          {t("watchdog.domains.explain")}
        </Typography>

        {enabled && domains.length > 0 && (
          <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
            {healthyCount > 0 && (
              <B4Badge
                label={`${healthyCount} ${t("watchdog.status.healthy")}`}
                color="primary"
                variant="outlined"
              />
            )}
            {queuedCount > 0 && (
              <B4Badge
                label={`${queuedCount} ${t("watchdog.status.queued")}`}
                color="info"
                variant="outlined"
              />
            )}
            {degradedCount > 0 && (
              <B4Badge
                label={`${degradedCount} ${t("watchdog.issues")}`}
                color="secondary"
                variant="outlined"
              />
            )}
          </Stack>
        )}

        {enabled && (
          <Stack direction="row" spacing={1} alignItems="center">
            <B4TextField
              size="small"
              value={newDomain}
              onChange={(e) => setNewDomain(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  handleAddDomain();
                }
              }}
              placeholder={t("watchdog.addPlaceholder")}
              sx={{ flex: 1, maxWidth: 400 }}
            />
            <B4PlusButton
              onClick={handleAddDomain}
              disabled={!newDomain.trim()}
            />
          </Stack>
        )}

        {enabled && domains.length === 0 && (
          <B4Alert severity="info">
            <Trans
              i18nKey="watchdog.noDomainsHint"
              components={{ a: <Link component={RouterLink} to="/settings/discovery" /> }}
            />
          </B4Alert>
        )}

        {domains.length > 0 && (
          <TableContainer sx={{ opacity: enabled ? 1 : 0.5 }}>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>{t("watchdog.table.domain")}</TableCell>
                  <TableCell>{t("watchdog.table.set")}</TableCell>
                  <TableCell>{t("watchdog.table.status")}</TableCell>
                  <TableCell>{t("watchdog.table.lastCheck")}</TableCell>
                  <TableCell>{t("watchdog.table.failures")}</TableCell>
                  <TableCell>{t("watchdog.table.error")}</TableCell>
                  <TableCell align="right">{t("watchdog.table.actions")}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {domains.map((domain) => (
                  <DomainRow
                    key={domain.domain}
                    domain={domain}
                    enabled={enabled}
                    moving={moving === domain.domain}
                    owner={
                      domain.owner_set_id
                        ? setConfigs.get(domain.owner_set_id)
                        : undefined
                    }
                    onForceCheck={onForceCheck}
                    onRemove={onRemove}
                    onMove={handleMove}
                  />
                ))}
              </TableBody>
            </Table>
          </TableContainer>
        )}
      </Stack>
    </B4Section>
  );
}
