import { Box, Link, Tooltip, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import { useOverlay } from "@/shared/hooks/useOverlay";

const statusColor: Record<string, string> = {
  pending: colors.state.warning,
  active: colors.state.success,
  hidden: colors.text.disabled,
  rejected: colors.state.error,
};

interface SetRefProps {
  id: string;
  version?: number;
  title?: string;
  status?: string;
  maxTitle?: number;
}

export const setHref = (id: string, version?: number) =>
  version ? `/sets/${encodeURIComponent(id)}/v/${String(version)}` : `/sets/${encodeURIComponent(id)}`;

export function SetRef({ id, version, title, status, maxTitle = 48 }: Readonly<SetRefProps>) {
  const { t } = useTranslation();
  const overlay = useOverlay();
  const shown = title && title.length > maxTitle ? title.slice(0, maxTitle - 1) + "…" : title;
  const tip = [title, `${id}${version ? `/${String(version)}` : ""}`, status ? t(`status.set.${status}`) : ""].filter(Boolean).join(" · ");
  return (
    <Tooltip title={tip}>
      <Link
        component="button"
        type="button"
        underline="hover"
        onClick={() => overlay.open(setHref(id, version))}
        sx={{ display: "inline-flex", alignItems: "baseline", gap: 0.75, textAlign: "left", maxWidth: "100%", minWidth: 0, color: colors.text.primary }}
      >
        {status && <Box component="span" sx={{ width: 7, height: 7, borderRadius: "50%", bgcolor: statusColor[status] ?? colors.text.disabled, flexShrink: 0, alignSelf: "center" }} />}
        {shown ? (
          <Typography component="span" variant="body2" sx={{ fontWeight: 500, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
            {shown}
          </Typography>
        ) : (
          <Typography component="span" variant="body2" sx={{ color: colors.text.disabled }}>
            {t("ref.deleted")}
          </Typography>
        )}
        <Typography component="span" sx={{ fontFamily: fonts.mono, fontSize: 11, color: colors.text.secondary, whiteSpace: "nowrap" }}>
          {id.slice(0, 8)}
          {version ? `/v${String(version)}` : ""}
        </Typography>
      </Link>
    </Tooltip>
  );
}
