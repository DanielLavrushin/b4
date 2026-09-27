import { Box, IconButton, Link, Tooltip, Typography } from "@mui/material";
import ContentCopyIcon from "@mui/icons-material/ContentCopy";
import { useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import { useSnackbar } from "@/app/SnackbarProvider";
import { useOverlay } from "@/shared/hooks/useOverlay";
import { useKnownKey } from "@/features/keys/KnownKeys";

interface KeyRefProps {
  hmac: string;
  label?: string;
  name?: string;
  tag?: string;
  banned?: boolean;
  trusted?: boolean;
  copy?: boolean;
}

export const keyHref = (hmac: string) => `/keys/${hmac}`;

const hue = (hmac: string) => parseInt(hmac.slice(0, 4), 16) % 360;

export function KeyRef({ hmac, label, name: givenName, tag: givenTag, banned: givenBanned, trusted: givenTrusted, copy = false }: Readonly<KeyRefProps>) {
  const { t } = useTranslation();
  const known = useKnownKey(hmac);
  const name = givenName ?? known?.name;
  const tag = givenTag ?? known?.tag;
  const banned = givenBanned ?? known?.banned;
  const trusted = givenTrusted ?? known?.trusted;
  const overlay = useOverlay();
  const { notify } = useSnackbar();
  const short = label ?? hmac.slice(0, 16);
  const state = banned ? t("ref.banned") : trusted ? t("ref.trusted") : "";
  const tip = [name, t("ref.authorLabel", { label: short }), hmac, state].filter(Boolean).join(" · ");
  const copyKey = () => {
    void navigator.clipboard.writeText(hmac).then(
      () => notify(t("app.copied"), "success"),
      () => undefined,
    );
  };
  return (
    <Box component="span" sx={{ display: "inline-flex", alignItems: "center", gap: 0.5, maxWidth: "100%", minWidth: 0 }}>
      <Tooltip title={tip}>
        <Link
          component="button"
          type="button"
          underline="hover"
          onClick={() => overlay.open(keyHref(hmac))}
          sx={{
            display: "inline-flex",
            alignItems: "center",
            gap: 0.75,
            minWidth: 0,
            color: banned ? colors.state.error : colors.text.primary,
          }}
        >
          <Box component="span" sx={{ width: 8, height: 8, borderRadius: "50%", bgcolor: `hsl(${String(hue(hmac))}, 55%, 55%)`, flexShrink: 0 }} />
          {name && (
            <Typography component="span" variant="body2" sx={{ fontWeight: 500, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
              {name}
            </Typography>
          )}
          <Typography component="span" sx={{ fontFamily: fonts.mono, fontSize: 12, color: name ? colors.text.secondary : "inherit", whiteSpace: "nowrap" }}>
            {hmac.slice(0, 8)}
          </Typography>
          {tag && (
            <Typography component="span" sx={{ fontSize: 10, fontWeight: 700, letterSpacing: "0.06em", px: 0.5, borderRadius: 0.5, bgcolor: colors.accent.primary, color: colors.text.primary }}>
              {t(`keys.tags.${tag}`, { defaultValue: tag }).toUpperCase()}
            </Typography>
          )}
          {banned && <Box component="span" sx={{ fontSize: 11, color: colors.state.error }}>{t("ref.banned")}</Box>}
          {!banned && trusted && <Box component="span" sx={{ fontSize: 11, color: colors.state.info }}>{t("ref.trusted")}</Box>}
        </Link>
      </Tooltip>
      {copy && (
        <Tooltip title={t("ref.copyKey")}>
          <IconButton size="small" onClick={copyKey} sx={{ p: 0.25 }}>
            <ContentCopyIcon sx={{ fontSize: 14 }} />
          </IconButton>
        </Tooltip>
      )}
    </Box>
  );
}
