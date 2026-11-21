import { useCallback, useEffect, useId, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  IconButton,
  Link,
  Popover,
  Stack,
  Typography,
  type PopoverActions,
} from "@mui/material";
import ReactMarkdown from "react-markdown";
import {
  AiIcon,
  CloseIcon,
  InfoOutlinedIcon,
  OpenInNewIcon,
} from "@b4.icons";
import { streamAi } from "@api/aiStream";
import { useAiStatus } from "@context/AiStatusProvider";
import { colors } from "@design";
import { docsUrl } from "@utils";

export interface B4HelpContent {
  info?: React.ReactNode;
  learnMore?: string;
  credit?: React.ReactNode;
  aiTopic?: string;
  aiContext?: Record<string, unknown>;
  aiQuestion?: string;
}

export interface B4HelpProps extends B4HelpContent {
  label?: string;
  fieldDoc?: string;
  value?: string | number | boolean;
  mt?: string;
}

export const useHelpVisible = ({
  info,
  learnMore,
  credit,
  aiTopic,
}: B4HelpContent): boolean => {
  const { enabled } = useAiStatus();
  return Boolean(info || learnMore || credit || (aiTopic && enabled));
};

export const B4Help = ({
  label,
  info,
  learnMore,
  credit,
  aiTopic,
  aiContext,
  aiQuestion,
  fieldDoc,
  value,
  mt,
}: B4HelpProps) => {
  const { t, i18n } = useTranslation();
  const { status, enabled, refresh } = useAiStatus();
  const [anchorEl, setAnchorEl] = useState<HTMLElement | null>(null);
  const [asked, setAsked] = useState(false);
  const [text, setText] = useState("");
  const [streaming, setStreaming] = useState(false);
  const [errMsg, setErrMsg] = useState("");
  const abortRef = useRef<AbortController | null>(null);
  const popoverRef = useRef<PopoverActions | null>(null);
  const panelId = useId();

  const open = Boolean(anchorEl);
  const canAsk = Boolean(aiTopic && enabled);
  const doc = [fieldDoc, typeof info === "string" ? info : ""]
    .filter(Boolean)
    .join(" ");
  const ctxJson = aiContext ? JSON.stringify(aiContext) : "";

  const ask = useCallback(async () => {
    if (!aiTopic) return;
    setAsked(true);
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
        topic: aiTopic,
        field_label: label ?? "",
        field_doc: doc,
        value: value === undefined ? "" : String(value),
        context_json: ctxJson,
        question: aiQuestion ?? "",
        language: i18n.language || "",
      },
      {
        onDelta: (chunk) => {
          if (abortRef.current === ctrl) setText((prev) => prev + chunk);
        },
        onError: (msg) => {
          if (abortRef.current === ctrl) setErrMsg(msg);
        },
        onDone: () => {
          if (abortRef.current === ctrl) setStreaming(false);
        },
      },
      ctrl.signal,
    );
  }, [status, refresh, aiTopic, label, doc, value, ctxJson, aiQuestion, i18n.language, t]);

  useEffect(() => {
    popoverRef.current?.updatePosition();
  }, [text, asked, errMsg]);

  useEffect(
    () => () => {
      abortRef.current?.abort();
    },
    [],
  );

  const handleClose = () => {
    abortRef.current?.abort();
    abortRef.current = null;
    setAnchorEl(null);
    setAsked(false);
    setStreaming(false);
    setText("");
    setErrMsg("");
  };

  if (!info && !learnMore && !credit && !canAsk) return null;

  const name = t("help.about", { label: label ?? "" });

  return (
    <>
      <IconButton
        size="small"
        onClick={(e) => setAnchorEl(e.currentTarget)}
        aria-label={name}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? panelId : undefined}
        data-help-trigger
        sx={{
          p: "4px",
          mt,
          flexShrink: 0,
          color: open ? colors.secondary : colors.text.secondary,
          opacity: open ? 1 : 0.7,
          "&:hover, &:focus-visible": { color: colors.secondary, opacity: 1 },
        }}
      >
        <InfoOutlinedIcon fontSize="small" />
      </IconButton>
      <Popover
        id={panelId}
        action={popoverRef}
        open={open}
        anchorEl={anchorEl}
        onClose={handleClose}
        anchorOrigin={{ vertical: "bottom", horizontal: "left" }}
        transformOrigin={{ vertical: "top", horizontal: "left" }}
        slotProps={{
          paper: {
            role: "dialog",
            "aria-label": name,
            sx: {
              width: 360,
              maxWidth: "calc(100vw - 32px)",
              maxHeight: "calc(100vh - 32px)",
              overflowY: "auto",
              p: 2,
              border: `1px solid ${colors.border.strong}`,
              bgcolor: colors.background.paper,
            },
          },
        }}
      >
        <Stack spacing={1.25}>
          <Stack direction="row" alignItems="flex-start" spacing={1}>
            <Typography variant="subtitle2" sx={{ flex: 1, fontWeight: 600 }}>
              {label}
            </Typography>
            <IconButton
              size="small"
              onClick={handleClose}
              aria-label={t("core.close")}
              sx={{ mt: "-4px", mr: "-4px" }}
            >
              <CloseIcon fontSize="small" />
            </IconButton>
          </Stack>

          {info && (
            <Typography
              variant="body2"
              component="div"
              sx={{
                color: colors.text.primary,
                lineHeight: 1.55,
                whiteSpace: "pre-line",
              }}
            >
              {info}
            </Typography>
          )}

          {credit && (
            <Typography
              variant="caption"
              component="div"
              sx={{ color: colors.text.secondary }}
            >
              {credit}
            </Typography>
          )}

          {asked && (
            <Box
              sx={{
                borderTop:
                  info || credit ? `1px solid ${colors.border.light}` : "none",
                pt: info || credit ? 1.25 : 0,
              }}
            >
              {errMsg && <Alert severity="warning">{errMsg}</Alert>}
              <Box
                aria-live="polite"
                sx={{
                  minHeight: 48,
                  maxHeight: 320,
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
                  <Stack direction="row" spacing={1} alignItems="center">
                    <CircularProgress size={14} />
                    <Typography
                      variant="caption"
                      sx={{ color: colors.text.secondary }}
                    >
                      {t("aiExplain.thinking")}
                    </Typography>
                  </Stack>
                ) : errMsg ? null : (
                  <Typography
                    variant="caption"
                    sx={{ color: colors.text.secondary }}
                  >
                    {t("aiExplain.empty")}
                  </Typography>
                )}
              </Box>
              <Typography
                variant="caption"
                sx={{ color: colors.text.secondary, display: "block", mt: 0.5 }}
              >
                {t("aiExplain.disclaimer")}
              </Typography>
            </Box>
          )}

          {(learnMore || canAsk) && (
            <Stack
              direction="row"
              alignItems="center"
              sx={{ flexWrap: "wrap", gap: "4px 16px" }}
            >
              {learnMore && (
                <Link
                  href={docsUrl(learnMore, i18n.language)}
                  target="_blank"
                  rel="noopener noreferrer"
                  underline="hover"
                  sx={{
                    display: "inline-flex",
                    alignItems: "center",
                    gap: 0.5,
                    fontSize: 13,
                    fontWeight: 500,
                  }}
                >
                  {t("help.learnMore")}
                  <OpenInNewIcon sx={{ fontSize: 14 }} />
                </Link>
              )}
              {canAsk && (
                <Button
                  size="small"
                  variant="text"
                  startIcon={<AiIcon fontSize="small" />}
                  onClick={() => {
                    void ask();
                  }}
                  disabled={streaming}
                  sx={{ color: colors.text.primary, px: 1 }}
                >
                  {asked ? t("aiExplain.regenerate") : t("help.askAi")}
                </Button>
              )}
            </Stack>
          )}
        </Stack>
      </Popover>
    </>
  );
};
