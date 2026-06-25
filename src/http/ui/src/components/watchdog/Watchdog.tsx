import { useState } from "react";
import { useTranslation, Trans } from "react-i18next";
import { Link as RouterLink } from "react-router";
import {
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
  WatchdogIcon,
  RefreshIcon,
  DeleteIcon,
  StartIcon,
  SuccessIcon,
  WarningIcon,
  ErrorIcon,
  TimerIcon,
} from "@b4.icons";
import { colors } from "@design";
import {
  B4Section,
  B4Alert,
  B4Badge,
  B4TextField,
  B4PlusButton,
} from "@b4.elements";
import { useWatchdog } from "@hooks/useWatchdog";
import { WatchdogDomainStatus } from "@models/watchdog";

function statusColor(
  status: string,
): "primary" | "secondary" | "error" | "info" {
  switch (status) {
    case "healthy":
      return "primary";
    case "degraded":
      return "secondary";
    case "escalating":
      return "info";
    case "queued":
      return "info";
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

function formatTime(iso: string | undefined): string {
  if (!iso || iso === "0001-01-01T00:00:00Z") return "-";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "-";
  const now = new Date();
  const diffSec = Math.floor((now.getTime() - d.getTime()) / 1000);
  if (diffSec < 60) return `${diffSec}s ago`;
  if (diffSec < 3600) return `${Math.floor(diffSec / 60)}m ago`;
  if (diffSec < 86400) return `${Math.floor(diffSec / 3600)}h ago`;
  return d.toLocaleString();
}

function DomainRow({
  domain,
  onForceCheck,
  onRemove,
}: Readonly<{
  domain: WatchdogDomainStatus;
  onForceCheck: (d: string) => void;
  onRemove: (d: string) => void;
}>) {
  const { t } = useTranslation();
  const isEscalating = domain.status === "escalating";

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
        <B4Badge
          label={t(`watchdog.status.${domain.status}`)}
          color={statusColor(domain.status)}
          variant="outlined"
        />
      </TableCell>
      <TableCell>
        <Typography variant="body2" color="text.secondary">
          {formatTime(domain.last_check)}
        </Typography>
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
        <Stack direction="row" spacing={0.5} justifyContent="flex-end">
          <Tooltip title={t("watchdog.forceCheck")}>
            <span>
              <IconButton
                size="small"
                onClick={() => onForceCheck(domain.domain)}
                disabled={isEscalating}
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

export function WatchdogMonitor() {
  const { t } = useTranslation();
  const { state, loading, forceCheck, addDomain, removeDomain, toggleEnabled, refresh } =
    useWatchdog();
  const [newDomain, setNewDomain] = useState("");

  const handleRefresh = () => {
    refresh().catch(() => {});
  };

  const handleAddDomain = () => {
    const domain = newDomain.trim();
    if (!domain) return;
    addDomain(domain)
      .then(() => setNewDomain(""))
      .catch(() => {});
  };

  if (loading || !state) {
    return null;
  }

  const domains = state.domains ?? [];
  const healthyCount = domains.filter((d) => d.status === "healthy").length;
  const degradedCount = domains.filter((d) => d.status !== "healthy").length;

  return (
    <B4Section
      title={t("watchdog.title")}
      description={t("watchdog.description")}
      icon={<WatchdogIcon />}
    >
      <Stack spacing={2}>
        <B4Alert icon={<WatchdogIcon />}>
          <Trans i18nKey="watchdog.alert" />{" "}
          {t("watchdog.inspiredBy")}{" "}
          <a
            href="https://github.com/belotserkovtsev/ladon"
            target="_blank"
            rel="noopener noreferrer"
          >
            belotserkovtsev/ladon
          </a>{" "}
          {t("watchdog.project")}
        </B4Alert>

        <Stack direction="row" justifyContent="space-between" alignItems="center">
          <Stack direction="row" spacing={1} alignItems="center">
            <B4Badge
              label={state.enabled ? t("watchdog.enabled") : t("watchdog.disabled")}
              color={state.enabled ? "primary" : "default"}
              onClick={() => {
                void toggleEnabled(!state.enabled);
              }}
              sx={{ cursor: "pointer" }}
            />
            {state.enabled && domains.length > 0 && (
              <>
                <B4Badge
                  label={`${healthyCount} ${t("watchdog.status.healthy")}`}
                  color="primary"
                  variant="outlined"
                />
                {degradedCount > 0 && (
                  <B4Badge
                    label={`${degradedCount} ${t("watchdog.issues")}`}
                    color="secondary"
                    variant="outlined"
                  />
                )}
              </>
            )}
          </Stack>
          <IconButton onClick={handleRefresh} size="small">
            <RefreshIcon sx={{ fontSize: 20 }} />
          </IconButton>
        </Stack>

        {!state.enabled && (
          <B4Alert severity="info">
            <Trans
              i18nKey="watchdog.disabledHint"
              components={{ link: <Link component={RouterLink} to="/settings/discovery" /> }}
            />
          </B4Alert>
        )}

        {state.enabled && (
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

        {state.enabled && domains.length === 0 && (
          <B4Alert severity="info">
            <Trans
              i18nKey="watchdog.noDomainsHint"
              components={{ link: <Link component={RouterLink} to="/settings/discovery" /> }}
            />
          </B4Alert>
        )}

        {domains.length > 0 && (
          <TableContainer>
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
                    onForceCheck={(d) => {
                      void forceCheck(d);
                    }}
                    onRemove={(d) => {
                      void removeDomain(d);
                    }}
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
