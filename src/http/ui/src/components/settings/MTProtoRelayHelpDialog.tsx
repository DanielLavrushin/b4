import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Box,
  Button,
  CircularProgress,
  IconButton,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableContainer,
  TableHead,
  TableRow,
  ToggleButton,
  ToggleButtonGroup,
  Tooltip,
  Typography,
} from "@mui/material";
import CheckIcon from "@mui/icons-material/Check";
import ContentCopyIcon from "@mui/icons-material/ContentCopy";
import HelpOutlineIcon from "@mui/icons-material/HelpOutline";
import RefreshIcon from "@mui/icons-material/Refresh";
import { B4Alert, B4Dialog } from "@b4.elements";
import { copyText } from "@utils";

interface RefreshOk {
  ok: true;
  count: number;
  dcs: Record<string, string>;
  direct?: Record<string, string>;
  direct_v6?: Record<string, string>;
}
interface RefreshErr {
  ok: false;
  error: string;
  direct?: Record<string, string>;
  direct_v6?: Record<string, string>;
}
type RefreshResult = RefreshOk | RefreshErr | null;

interface RelayInfo {
  host: string;
  hostIsV6: boolean;
  basePort: number;
}

type Family = "v4" | "v6";

interface Props {
  open: boolean;
  onClose: () => void;
  relayInfo: RelayInfo | null;
  refreshResult: RefreshResult;
  refreshing: boolean;
  onRefresh: () => void;
}

const buildSocatCmd = (
  port: number,
  addr: string,
  upstreamV6: boolean,
  listenV6: boolean,
) => {
  const listen = listenV6 ? "TCP6-LISTEN" : "TCP-LISTEN";
  const upstream = upstreamV6 ? "TCP6" : "TCP";
  return `socat ${listen}:${port},fork,reuseaddr ${upstream}:${addr} &`;
};

export const MTProtoRelayHelpDialog = ({
  open,
  onClose,
  relayInfo,
  refreshResult,
  refreshing,
  onRefresh,
}: Props) => {
  const { t } = useTranslation();
  const [copiedAll, setCopiedAll] = useState(false);
  const [copiedRow, setCopiedRow] = useState<number | null>(null);
  const [family, setFamily] = useState<Family>("v4");

  const addrsV6 = refreshResult?.direct_v6;
  const hasV6 = !!addrsV6 && Object.keys(addrsV6).length > 0;

  useEffect(() => {
    if (!hasV6) setFamily("v4");
  }, [hasV6]);

  const upstreamV6 = family === "v6" && hasV6;
  const listenV6 = !!relayInfo?.hostIsV6;

  const mappings = useMemo(() => {
    const addrsV4 = refreshResult?.ok
      ? (refreshResult.direct ?? refreshResult.dcs)
      : refreshResult?.direct;
    const addrs = upstreamV6 ? addrsV6 : addrsV4;
    if (!relayInfo || !addrs) return [];
    return Object.entries(addrs)
      .map(([id, addr]) => {
        const absDc = Math.abs(Number(id));
        return { dc: absDc, addr };
      })
      .filter((m) => m.dc !== 203)
      .map((m) => ({ ...m, port: relayInfo.basePort + m.dc - 1 }))
      .sort((a, b) => a.dc - b.dc);
  }, [relayInfo, refreshResult, upstreamV6, addrsV6]);

  const allCommands = useMemo(
    () =>
      mappings
        .map((m) => buildSocatCmd(m.port, m.addr, upstreamV6, listenV6))
        .join("\n"),
    [mappings, upstreamV6, listenV6],
  );

  const portsList = useMemo(
    () => mappings.map((m) => m.port).join(", "),
    [mappings],
  );

  const handleCopyAll = async () => {
    if (!allCommands) return;
    if (await copyText(allCommands)) {
      setCopiedAll(true);
      setTimeout(() => setCopiedAll(false), 1500);
    }
  };

  const handleCopyRow = async (idx: number, cmd: string) => {
    if (await copyText(cmd)) {
      setCopiedRow(idx);
      setTimeout(() => setCopiedRow(null), 1500);
    }
  };

  const fetchFailed =
    relayInfo && !refreshing && refreshResult && !refreshResult.ok;
  const fetchPending =
    relayInfo && refreshing && !refreshResult?.ok;

  return (
    <B4Dialog
      open={open}
      onClose={onClose}
      fullWidth
      maxWidth="md"
      title={t("settings.MTProto.dcRelayHelpTitle")}
      icon={<HelpOutlineIcon />}
      actions={
        <>
          <Button onClick={onClose}>{t("core.close")}</Button>
          <Box sx={{ flex: 1 }} />
          <Button
            variant="outlined"
            startIcon={
              <RefreshIcon
                sx={{
                  animation: refreshing ? "spin 1s linear infinite" : "none",
                  "@keyframes spin": {
                    from: { transform: "rotate(0deg)" },
                    to: { transform: "rotate(360deg)" },
                  },
                }}
              />
            }
            onClick={onRefresh}
            disabled={refreshing}
          >
            {refreshing
              ? t("settings.MTProto.refreshingDCs")
              : t("settings.MTProto.refreshDCs")}
          </Button>
          <Button
            variant="contained"
            startIcon={copiedAll ? <CheckIcon /> : <ContentCopyIcon />}
            onClick={() => void handleCopyAll()}
            disabled={!allCommands}
          >
            {copiedAll
              ? t("core.copied")
              : t("settings.MTProto.dcRelayHelpCopyAll")}
          </Button>
        </>
      }
    >
      <Stack gap={2} sx={{ mt: 1 }}>
        <Typography variant="body2" color="text.secondary">
          {t("settings.MTProto.dcRelayHelpIntro")}
        </Typography>

        <Typography
          variant="body2"
          color="text.secondary"
          dangerouslySetInnerHTML={{
            __html: t("settings.MTProto.relaySetup"),
          }}
        />

        {hasV6 && (
          <Stack direction="row" alignItems="center" gap={1.5}>
            <Typography variant="body2" color="text.secondary">
              {t("settings.MTProto.dcRelayHelpFamily")}
            </Typography>
            <ToggleButtonGroup
              exclusive
              size="small"
              value={family}
              onChange={(_, v: Family | null) => v && setFamily(v)}
            >
              <ToggleButton value="v4">
                {t("settings.MTProto.dcRelayHelpFamilyV4")}
              </ToggleButton>
              <ToggleButton value="v6">
                {t("settings.MTProto.dcRelayHelpFamilyV6")}
              </ToggleButton>
            </ToggleButtonGroup>
          </Stack>
        )}

        {!relayInfo && (
          <B4Alert severity="info">
            {t("settings.MTProto.dcRelayHelpEmpty", {
              example: "vps.example.com:7007",
            })}
          </B4Alert>
        )}

        {fetchPending && (
          <Stack direction="row" alignItems="center" gap={1}>
            <CircularProgress size={16} />
            <Typography variant="body2" color="text.secondary">
              {t("settings.MTProto.dcRelayHelpLoading")}
            </Typography>
          </Stack>
        )}

        {fetchFailed && (
          <B4Alert severity={mappings.length > 0 ? "warning" : "error"}>
            {t(
              mappings.length > 0
                ? "settings.MTProto.dcRelayHelpRefreshFailed"
                : "settings.MTProto.dcRelayHelpFailed",
            )}
          </B4Alert>
        )}

        {relayInfo && mappings.length > 0 && (
          <>
            <TableContainer>
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell sx={{ width: 60 }}>
                      {t("settings.MTProto.dcRelayHelpDc")}
                    </TableCell>
                    <TableCell sx={{ width: 100 }}>
                      {t("settings.MTProto.dcRelayHelpRelayPort")}
                    </TableCell>
                    <TableCell>
                      {t("settings.MTProto.dcRelayHelpUpstream")}
                    </TableCell>
                    <TableCell>
                      {t("settings.MTProto.dcRelayHelpCommand")}
                    </TableCell>
                    <TableCell sx={{ width: 48 }} />
                  </TableRow>
                </TableHead>
                <TableBody>
                  {mappings.map((m, idx) => {
                    const cmd = buildSocatCmd(
                      m.port,
                      m.addr,
                      upstreamV6,
                      listenV6,
                    );
                    return (
                      <TableRow key={m.dc}>
                        <TableCell>DC{m.dc}</TableCell>
                        <TableCell sx={{ fontFamily: "monospace" }}>
                          {m.port}
                        </TableCell>
                        <TableCell sx={{ fontFamily: "monospace" }}>
                          {m.addr}
                        </TableCell>
                        <TableCell
                          sx={{
                            fontFamily: "monospace",
                            fontSize: "0.75rem",
                            whiteSpace: "nowrap",
                            overflow: "hidden",
                            textOverflow: "ellipsis",
                            maxWidth: 320,
                          }}
                        >
                          {cmd}
                        </TableCell>
                        <TableCell>
                          <Tooltip
                            title={
                              copiedRow === idx
                                ? t("core.copied")
                                : t("core.copy")
                            }
                          >
                            <IconButton
                              size="small"
                              onClick={() => void handleCopyRow(idx, cmd)}
                            >
                              {copiedRow === idx ? (
                                <CheckIcon fontSize="small" color="success" />
                              ) : (
                                <ContentCopyIcon fontSize="small" />
                              )}
                            </IconButton>
                          </Tooltip>
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </TableContainer>
            {upstreamV6 && (
              <B4Alert severity="info">
                {t("settings.MTProto.dcRelayHelpV6Note")}
              </B4Alert>
            )}
            <B4Alert severity="info">
              {t("settings.MTProto.dcRelayHelpHint", { ports: portsList })}
            </B4Alert>
          </>
        )}
      </Stack>
    </B4Dialog>
  );
};

export default MTProtoRelayHelpDialog;
