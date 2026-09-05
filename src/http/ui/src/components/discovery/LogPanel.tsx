import { useEffect, useRef, useState } from "react";
import { Box, Typography, Button } from "@mui/material";
import { ClearIcon, LogsIcon, CloseIcon, DownloadIcon } from "@b4.icons";
import { colors } from "@design";
import { B4Dialog } from "@common/B4Dialog";
import { B4RunLine } from "@common/B4RunProgress";
import { useTranslation } from "react-i18next";
import { discoveryApi } from "@api/discovery";

const downloadText = (text: string) => {
  const stamp = new Date().toISOString().slice(0, 16).replace(/[-:T]/g, "");
  const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
  const a = document.createElement("a");
  a.href = url;
  a.download = `b4-discovery-${stamp}.txt`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
};

interface DiscoveryLogLineProps {
  logs: string[];
  connected: boolean;
  onOpen: () => void;
}

export const DiscoveryLogLine = ({
  logs,
  connected,
  onOpen,
}: DiscoveryLogLineProps) => {
  const { t } = useTranslation();
  const last = logs.at(-1) ?? "";

  return (
    <B4RunLine
      text={last}
      placeholder={t("discovery.logs.waiting")}
      live={connected}
      action={
        <Button size="small" startIcon={<LogsIcon />} onClick={onOpen} sx={{ textTransform: "none", flexShrink: 0 }}>
          {t("discovery.run.showLog")}
        </Button>
      }
    />
  );
};

interface DiscoveryLogDialogProps {
  open: boolean;
  logs: string[];
  onClose: () => void;
  onClear: () => void;
}

export const DiscoveryLogDialog = ({
  open,
  logs,
  onClose,
  onClear,
}: DiscoveryLogDialogProps) => {
  const { t } = useTranslation();
  const scrollRef = useRef<HTMLDivElement>(null);
  const [saved, setSaved] = useState<string[]>([]);
  const [fetched, setFetched] = useState(false);
  const lines = logs.length > 0 ? logs : saved;

  useEffect(() => {
    if (!open) {
      setFetched(false);
      setSaved([]);
    }
  }, [open]);

  useEffect(() => {
    if (!open || logs.length > 0 || fetched) return;
    setFetched(true);
    let active = true;
    discoveryApi
      .log()
      .then((text) => {
        if (active) setSaved(text.split("\n").filter((l) => l.length > 0));
      })
      .catch(() => {
        if (active) setSaved([]);
      });
    return () => {
      active = false;
    };
  }, [open, logs.length, fetched]);

  const clear = () => {
    setSaved([]);
    setFetched(true);
    onClear();
  };

  useEffect(() => {
    if (scrollRef.current && open) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight;
    }
  }, [lines, open]);

  const download = () => {
    void discoveryApi
      .log()
      .then((text) => downloadText(text))
      .catch(() => downloadText(lines.join("\n")));
  };

  return (
    <B4Dialog
      title={t("discovery.logs.title")}
      icon={<LogsIcon />}
      open={open}
      onClose={onClose}
      fullWidth
      maxWidth="xl"
      actions={
        <>
          <Button onClick={clear} startIcon={<ClearIcon />} size="small">
            {t("discovery.logs.clear")}
          </Button>
          <Button
            onClick={download}
            startIcon={<DownloadIcon />}
            size="small"
            disabled={lines.length === 0}
          >
            {t("discovery.logs.download")}
          </Button>
          <Box sx={{ flex: 1 }} />
          <Button
            onClick={onClose}
            variant="contained"
            startIcon={<CloseIcon />}
          >
            {t("core.close")}
          </Button>
        </>
      }
    >
      <div
        ref={scrollRef}
        style={{
          height: "60vh",
          overflowY: "auto",
          backgroundColor: colors.background.dark,
          fontFamily: "monospace",
          fontSize: 12,
          padding: 16,
        }}
      >
        {lines.length === 0 ? (
          <Typography sx={{ color: colors.text.disabled, fontStyle: "italic" }}>
            {t("discovery.logs.waiting")}
          </Typography>
        ) : (
          lines.map((line, i) => (
            <div
              key={`${i}-${line.slice(0, 24)}`}
              style={{
                color: getLogColor(line),
                whiteSpace: "pre-wrap",
                wordBreak: "break-word",
                lineHeight: 1.6,
              }}
            >
              {line}
            </div>
          ))
        )}
      </div>
    </B4Dialog>
  );
};

function getLogColor(line: string): string {
  const lower = line.toLowerCase();
  if (lower.includes("success") || line.includes("✓") || lower.includes("best"))
    return colors.secondary;
  if (lower.includes("failed") || line.includes("✗") || lower.includes("fail"))
    return colors.primary;
  if (lower.includes("phase")) return colors.text.secondary;
  return colors.text.primary;
}
