import { Fragment, useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { Link as RouterLink } from "react-router";
import {
  Box,
  Collapse,
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
  CollapseIcon,
  ExpandIcon,
  HideIcon,
  SetsIcon,
  StartIcon,
} from "@b4.icons";
import { B4Alert, B4Badge, B4Section } from "@b4.elements";
import { colors } from "@design";
import { useSnackbar } from "@context/SnackbarProvider";
import {
  SetWatchStatus,
  URLWatchStatus,
  WatchdogCheckOutcome,
  setWatchTone,
  urlWatchTone,
} from "@models/watchdog";
import {
  describeCodedError,
  formatBytes,
  formatSpeed,
  presetLabel,
  probeUrlDisplay,
} from "@utils";
import { clockTime, fullTime, isFuture, timeAgo } from "./format";

const ACTIVE_STATES = new Set(["heal_queued", "healing"]);
const ISSUE_STATES = new Set(["degraded", "cooldown", "unverifiable", "gave_up"]);

interface SetsSectionProps {
  sets: SetWatchStatus[];
  enabled: boolean;
  setNames: Map<string, string>;
  onCheck: (setId: string) => Promise<WatchdogCheckOutcome | undefined>;
  onTurnOff: (setId: string) => Promise<void>;
}

const Muted = ({ children }: Readonly<{ children: React.ReactNode }>) => (
  <Typography variant="body2" color="text.secondary">
    {children}
  </Typography>
);

function urlHandler(
  url: URLWatchStatus,
  setNames: Map<string, string>,
  t: (key: string, opts?: Record<string, unknown>) => string,
): string {
  if (url.escalated_to) {
    return t("watchdog.urls.escalatesTo", {
      name: setNames.get(url.escalated_to) ?? url.escalated_to,
    });
  }
  if (url.owner_set_id || url.owner_set_name) {
    return t("watchdog.urls.handledBy", {
      name:
        url.owner_set_name ||
        setNames.get(url.owner_set_id ?? "") ||
        url.owner_set_id,
    });
  }
  return "";
}

function urlResponse(url: URLWatchStatus): string {
  const parts: string[] = [];
  if (url.status_code) parts.push(`HTTP ${url.status_code}`);
  if (url.bytes_read) parts.push(formatBytes(url.bytes_read));
  if (url.speed) {
    const speed = formatSpeed(url.speed);
    if (speed) parts.push(speed);
  }
  return parts.join(", ");
}

function UrlTable({
  urls,
  setNames,
}: Readonly<{ urls: URLWatchStatus[]; setNames: Map<string, string> }>) {
  const { t } = useTranslation();
  if (urls.length === 0) {
    return <Muted>{t("watchdog.urls.empty")}</Muted>;
  }
  return (
    <Table size="small">
      <TableHead>
        <TableRow>
          <TableCell>{t("watchdog.urls.address")}</TableCell>
          <TableCell>{t("watchdog.urls.status")}</TableCell>
          <TableCell>{t("watchdog.urls.handler")}</TableCell>
          <TableCell>{t("watchdog.urls.response")}</TableCell>
          <TableCell>{t("watchdog.urls.lastCheck")}</TableCell>
          <TableCell>{t("watchdog.urls.error")}</TableCell>
        </TableRow>
      </TableHead>
      <TableBody>
        {urls.map((u) => {
          const handler = urlHandler(u, setNames, t);
          const response = urlResponse(u);
          return (
            <TableRow
              key={u.url}
              sx={{ "&:last-child td, &:last-child th": { border: 0 } }}
            >
              <TableCell>
                <Typography variant="body2" sx={{ wordBreak: "break-all" }}>
                  {probeUrlDisplay(u.url)}
                </Typography>
              </TableCell>
              <TableCell>
                <B4Badge
                  label={t(`watchdog.urlStatus.${u.status}`, {
                    defaultValue: u.status,
                  })}
                  color={urlWatchTone(u.status)}
                  variant="outlined"
                />
              </TableCell>
              <TableCell>
                {handler ? (
                  <Typography variant="body2">{handler}</Typography>
                ) : (
                  <Muted>-</Muted>
                )}
              </TableCell>
              <TableCell>
                {response ? (
                  <Typography variant="body2">{response}</Typography>
                ) : (
                  <Muted>-</Muted>
                )}
              </TableCell>
              <TableCell>
                <Tooltip title={fullTime(u.last_check)}>
                  <Typography variant="body2" color="text.secondary">
                    {timeAgo(t, u.last_check)}
                  </Typography>
                </Tooltip>
              </TableCell>
              <TableCell>
                {u.last_error ? (
                  <Tooltip title={u.last_error}>
                    <B4Badge
                      label={u.last_error}
                      color="error"
                      variant="outlined"
                      sx={{ maxWidth: 250 }}
                    />
                  </Tooltip>
                ) : (
                  <Muted>-</Muted>
                )}
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}

function SetRow({
  entry,
  enabled,
  setNames,
  onCheck,
  onTurnOff,
}: Readonly<{
  entry: SetWatchStatus;
  enabled: boolean;
  setNames: Map<string, string>;
  onCheck: (setId: string) => void;
  onTurnOff: (setId: string) => void;
}>) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const urls = entry.urls ?? [];
  const busy = ACTIVE_STATES.has(entry.status);
  const cooling = isFuture(entry.cooldown_until);
  const reasonText = entry.reason
    ? t(`watchdog.reason.${entry.reason}`, { defaultValue: entry.reason })
    : "";

  return (
    <Fragment>
      <TableRow sx={{ "& > td": { borderBottom: open ? 0 : undefined } }}>
        <TableCell sx={{ width: 40, pr: 0 }}>
          <IconButton
            size="small"
            onClick={() => setOpen((v) => !v)}
            aria-label={t("watchdog.sets.toggleUrls")}
          >
            {open ? (
              <CollapseIcon sx={{ fontSize: 18 }} />
            ) : (
              <ExpandIcon sx={{ fontSize: 18 }} />
            )}
          </IconButton>
        </TableCell>
        <TableCell>
          <Link
            component={RouterLink}
            to={`/sets/${encodeURIComponent(entry.set_id)}?tab=discovery`}
            sx={{ fontWeight: 600 }}
          >
            {entry.set_name || entry.set_id}
          </Link>
          <Typography
            variant="caption"
            sx={{
              display: "block",
              color: colors.text.secondary,
              whiteSpace: "nowrap",
            }}
          >
            {t("watchdog.sets.addresses", { count: urls.length })}
          </Typography>
        </TableCell>
        <TableCell>
          <B4Badge
            label={t(`watchdog.setStatus.${entry.status}`, {
              defaultValue: entry.status,
            })}
            color={setWatchTone(entry.status)}
            variant="outlined"
          />
        </TableCell>
        <TableCell sx={{ maxWidth: 360 }}>
          <Stack spacing={0.25}>
            {reasonText && <Typography variant="body2">{reasonText}</Typography>}
            {entry.consecutive_failures > 0 && (
              <Typography variant="caption" color="text.secondary">
                {t("watchdog.sets.failedChecks", {
                  count: entry.consecutive_failures,
                })}
              </Typography>
            )}
            {entry.last_error && (
              <Tooltip title={entry.last_error}>
                <Typography
                  variant="caption"
                  noWrap
                  sx={{ color: colors.state.error, display: "block" }}
                >
                  {entry.last_error}
                </Typography>
              </Tooltip>
            )}
            {!reasonText &&
              entry.consecutive_failures === 0 &&
              !entry.last_error && <Muted>-</Muted>}
          </Stack>
        </TableCell>
        <TableCell>
          <Tooltip title={fullTime(entry.last_check)}>
            <Typography variant="body2" color="text.secondary">
              {timeAgo(t, entry.last_check)}
            </Typography>
          </Tooltip>
        </TableCell>
        <TableCell>
          <Tooltip title={fullTime(entry.last_heal)}>
            <Typography variant="body2" color="text.secondary">
              {timeAgo(t, entry.last_heal)}
            </Typography>
          </Tooltip>
          {entry.last_heal_preset && (
            <Typography variant="caption" sx={{ display: "block" }}>
              {presetLabel(entry.last_heal_preset, t)}
            </Typography>
          )}
        </TableCell>
        <TableCell>
          {entry.heal_failures > 0 ? (
            <B4Badge
              label={`${entry.heal_failures}`}
              color="warning"
              variant="outlined"
            />
          ) : (
            <Muted>0</Muted>
          )}
        </TableCell>
        <TableCell>
          {cooling ? (
            <Tooltip title={fullTime(entry.cooldown_until)}>
              <Typography
                variant="body2"
                color="text.secondary"
                sx={{ whiteSpace: "nowrap" }}
              >
                {t("watchdog.sets.until", {
                  time: clockTime(entry.cooldown_until),
                })}
              </Typography>
            </Tooltip>
          ) : (
            <Muted>-</Muted>
          )}
        </TableCell>
        <TableCell align="right">
          <Stack direction="row" spacing={0.5} justifyContent="flex-end">
            <Tooltip
              title={
                enabled
                  ? t("watchdog.sets.checkNow")
                  : t("watchdog.forceCheckDisabled")
              }
            >
              <span>
                <IconButton
                  size="small"
                  onClick={() => onCheck(entry.set_id)}
                  disabled={!enabled || busy}
                >
                  <StartIcon sx={{ fontSize: 18 }} />
                </IconButton>
              </span>
            </Tooltip>
            <Tooltip title={t("watchdog.sets.turnOff")}>
              <IconButton
                size="small"
                color="error"
                onClick={() => onTurnOff(entry.set_id)}
              >
                <HideIcon sx={{ fontSize: 18 }} />
              </IconButton>
            </Tooltip>
          </Stack>
        </TableCell>
      </TableRow>
      <TableRow>
        <TableCell colSpan={9} sx={{ py: 0, borderBottom: open ? undefined : 0 }}>
          <Collapse in={open} timeout="auto" unmountOnExit>
            <Box
              sx={{
                my: 1.5,
                p: 1,
                border: `1px solid ${colors.border.default}`,
                borderRadius: 1,
                bgcolor: colors.background.dark,
              }}
            >
              <UrlTable urls={urls} setNames={setNames} />
            </Box>
          </Collapse>
        </TableCell>
      </TableRow>
    </Fragment>
  );
}

export function SetsSection({
  sets,
  enabled,
  setNames,
  onCheck,
  onTurnOff,
}: Readonly<SetsSectionProps>) {
  const { t } = useTranslation();
  const { showError, showSuccess } = useSnackbar();

  const healthy = sets.filter((s) => s.status === "healthy").length;
  const working = sets.filter((s) => ACTIVE_STATES.has(s.status)).length;
  const issues = sets.filter((s) => ISSUE_STATES.has(s.status)).length;

  const nameOf = (id: string) =>
    sets.find((s) => s.set_id === id)?.set_name ?? setNames.get(id) ?? id;

  const handleCheck = (setId: string) => {
    onCheck(setId)
      .then((outcome) => {
        const name = nameOf(setId);
        if (outcome === "master_off") {
          showError(t("watchdog.sets.checkMasterOff", { name }));
        } else if (outcome === "healing") {
          showError(t("watchdog.sets.checkHealing", { name }));
        } else {
          showSuccess(t("watchdog.sets.checkScheduled", { name }));
        }
      })
      .catch((e: unknown) =>
        showError(describeCodedError(e, t, "watchdog.errors")),
      );
  };

  const handleTurnOff = (setId: string) => {
    const name = nameOf(setId);
    onTurnOff(setId)
      .then(() => showSuccess(t("watchdog.sets.turnedOff", { name })))
      .catch((e: unknown) =>
        showError(describeCodedError(e, t, "watchdog.errors")),
      );
  };

  return (
    <B4Section
      title={t("watchdog.sets.title")}
      description={t("watchdog.sets.description")}
      icon={<SetsIcon />}
    >
      <Stack spacing={2}>
        {sets.length > 0 && (
          <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
            {healthy > 0 && (
              <B4Badge
                label={`${healthy} ${t("watchdog.setStatus.healthy")}`}
                color="success"
                variant="outlined"
              />
            )}
            {working > 0 && (
              <B4Badge
                label={`${working} ${t("watchdog.setStatus.healing")}`}
                color="info"
                variant="outlined"
              />
            )}
            {issues > 0 && (
              <B4Badge
                label={`${issues} ${t("watchdog.issues")}`}
                color="warning"
                variant="outlined"
              />
            )}
          </Stack>
        )}

        {sets.length === 0 ? (
          <B4Alert severity="info">
            <Trans
              i18nKey="watchdog.sets.empty"
              components={{ a: <Link component={RouterLink} to="/sets" /> }}
            />
          </B4Alert>
        ) : (
          <TableContainer sx={{ opacity: enabled ? 1 : 0.5 }}>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell />
                  <TableCell>{t("watchdog.sets.set")}</TableCell>
                  <TableCell>{t("watchdog.table.status")}</TableCell>
                  <TableCell>{t("watchdog.sets.details")}</TableCell>
                  <TableCell>{t("watchdog.table.lastCheck")}</TableCell>
                  <TableCell>{t("watchdog.sets.lastHeal")}</TableCell>
                  <TableCell>{t("watchdog.sets.healFailures")}</TableCell>
                  <TableCell>{t("watchdog.sets.cooldown")}</TableCell>
                  <TableCell align="right">
                    {t("watchdog.table.actions")}
                  </TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {sets.map((entry) => (
                  <SetRow
                    key={entry.set_id}
                    entry={entry}
                    enabled={enabled}
                    setNames={setNames}
                    onCheck={handleCheck}
                    onTurnOff={handleTurnOff}
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
