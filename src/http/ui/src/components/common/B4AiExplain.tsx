import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Alert,
  Box,
  CircularProgress,
  IconButton,
  Popover,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import ReactMarkdown from "react-markdown";
import { AiIcon, CloseIcon, RefreshIcon } from "@b4.icons";
import { streamAi } from "@api/aiStream";
import { useAiStatus } from "@context/AiStatusProvider";
import { colors } from "@design";

export const aiHoverRevealSx = {
  "& [data-ai-trigger]": {
    opacity: 0,
    transition: "opacity 120ms ease",
  },
  "&:hover [data-ai-trigger]": {
    opacity: 1,
  },
  "& [data-ai-trigger]:focus-visible": {
    opacity: 1,
  },
  '& [data-ai-trigger][data-ai-open="true"]': {
    opacity: 1,
  },
};

export interface B4AiExplainProps {
  topic: string;
  fieldLabel?: string;
  fieldDoc?: string;
  value?: string | number | boolean;
  contextJson?: string;
  context?: Record<string, unknown>;
  question?: string;
  size?: "small" | "medium";
}

export const B4AiExplain = ({
  topic,
  fieldLabel,
  fieldDoc,
  value,
  contextJson,
  context,
  question,
  size = "small",
}: B4AiExplainProps) => {
  const { t, i18n } = useTranslation();
  const { status, enabled, refresh } = useAiStatus();
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);
  const [text, setText] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [errMsg, setErrMsg] = useState("");
  const abortRef = useRef<AbortController | null>(null);

  const open = Boolean(anchorEl);

  const ctxJson = context ? JSON.stringify(context) : (contextJson ?? "");

  const start = useCallback(async () => {
    setText("");
    setErrMsg("");
    const s = status ?? (await refresh());
    if (!s?.ready) {
      setErrMsg(s?.not_ready_reason || t("aiExplain.notReady"));
      return;
    }
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    setStreaming(true);
    await streamAi(
      "/api/ai/explain",
      {
        topic,
        field_label: fieldLabel ?? "",
        field_doc: fieldDoc ?? "",
        value: value === undefined ? "" : String(value),
        context_json: ctxJson,
        question: question ?? "",
        language: i18n.language || "",
      },
      {
        onDelta: (chunk) => setText((prev) => prev + chunk),
        onError: (msg) => setErrMsg(msg),
        onDone: () => setStreaming(false),
      },
      ctrl.signal,
    );
  }, [status, refresh, topic, fieldLabel, fieldDoc, value, ctxJson, question, i18n.language, t]);

  useEffect(() => {
    if (!open) {
      abortRef.current?.abort();
      abortRef.current = null;
      return;
    }
    void start();
  }, [open, start]);

  useEffect(
    () => () => {
      abortRef.current?.abort();
    },
    [],
  );

  const handleClose = () => setAnchorEl(null);

  if (!enabled) {
    return null;
  }

  return (
    <>
      <Tooltip title={t("aiExplain.tooltip")}>
        <span>
          <IconButton
            data-ai-trigger
            data-ai-open={open ? "true" : undefined}
            size={size}
            onClick={(e) => setAnchorEl(e.currentTarget)}
            aria-label={t("aiExplain.tooltip")}
            sx={{ color: colors.primary }}
          >
            <AiIcon fontSize={size === "small" ? "small" : "medium"} />
          </IconButton>
        </span>
      </Tooltip>
      <Popover
        open={open}
        anchorEl={anchorEl}
        onClose={handleClose}
        anchorOrigin={{ vertical: "bottom", horizontal: "right" }}
        transformOrigin={{ vertical: "top", horizontal: "right" }}
        slotProps={{
          paper: {
            sx: {
              width: 420,
              maxWidth: "90vw",
              p: 2,
              border: `1px solid ${colors.border.default}`,
              bgcolor: colors.background.paper,
            },
          },
        }}
      >
        <Stack spacing={1.5}>
          <Stack direction="row" spacing={1} sx={{
            alignItems: "center"
          }}>
            <AiIcon fontSize="small" sx={{ color: colors.secondary }} />
            <Typography variant="subtitle2" sx={{ flex: 1 }}>
              {t("aiExplain.title", { topic })}
            </Typography>
            <Tooltip title={t("aiExplain.regenerate")}>
              <span>
                <IconButton
                  size="small"
                  onClick={() => {
                    void start();
                  }}
                  disabled={streaming}
                >
                  <RefreshIcon fontSize="small" />
                </IconButton>
              </span>
            </Tooltip>
            <IconButton size="small" onClick={handleClose}>
              <CloseIcon fontSize="small" />
            </IconButton>
          </Stack>

          {errMsg && <Alert severity="warning">{errMsg}</Alert>}

          <Box
            sx={{
              minHeight: 80,
              maxHeight: 360,
              overflowY: "auto",
              fontSize: 14,
              lineHeight: 1.5,
              color: colors.text.primary,
              "& p": { mt: 0, mb: 1 },
              "& ul, & ol": { pl: 2.5, mt: 0, mb: 1 },
              "& code": {
                bgcolor: colors.background.dark,
                px: 0.5,
                borderRadius: 0.5,
              },
            }}
          >
            {text ? (
              <ReactMarkdown>{text}</ReactMarkdown>
            ) : streaming ? (
              <Stack direction="row" spacing={1} sx={{
                alignItems: "center"
              }}>
                <CircularProgress size={14} />
                <Typography
                  variant="caption"
                  sx={{ color: colors.text.secondary }}
                >
                  {t("aiExplain.thinking")}
                </Typography>
              </Stack>
            ) : !errMsg ? (
              <Typography
                variant="caption"
                sx={{ color: colors.text.secondary }}
              >
                {t("aiExplain.empty")}
              </Typography>
            ) : null}
          </Box>

          <Typography variant="caption" sx={{ color: colors.text.secondary }}>
            {t("aiExplain.disclaimer")}
          </Typography>
        </Stack>
      </Popover>
    </>
  );
};

export default B4AiExplain;
