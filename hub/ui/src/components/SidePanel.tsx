import { Box, Link } from "@mui/material";
import DescriptionIcon from "@mui/icons-material/DescriptionOutlined";
import GitHubIcon from "@mui/icons-material/GitHub";
import { useTranslation } from "react-i18next";
import { colors, fonts, radiusPx } from "@design";
import { useSession } from "@/context/SessionProvider";

const REPO = "DanielLavrushin/b4";
const REPO_URL = "https://github.com/daniellavrushin/b4";
const DOCS_URL = "https://docs.b4core.app";

const sideLinkSx = {
  display: "flex",
  alignItems: "center",
  gap: "8px",
  p: "6px 8px",
  borderRadius: `${radiusPx.sm}px`,
  color: colors.text.primary,
  textDecoration: "none",
  fontSize: 12,
  transition: "background-color 150ms ease",
  "&:hover": {
    backgroundColor: colors.background.hover,
    color: colors.text.primary,
    textDecoration: "none",
  },
};

const sideLinkLabelSx = {
  fontWeight: 500,
  overflow: "hidden",
  textOverflow: "ellipsis",
  whiteSpace: "nowrap",
  flex: 1,
  minWidth: 0,
};

const sideLinkArrowSx = {
  ml: "auto",
  color: colors.text.secondary,
  opacity: 0.5,
  fontSize: 11,
  flexShrink: 0,
};

export function SidePanel() {
  const { t, i18n } = useTranslation();
  const { version } = useSession();
  const docsUrl = i18n.language.startsWith("ru") ? `${DOCS_URL}/ru/` : `${DOCS_URL}/`;

  return (
    <Box sx={{ mt: "auto", p: "10px 12px", display: "flex", flexDirection: "column", gap: "8px" }}>
      <Link href={docsUrl} target="_blank" rel="noopener noreferrer" sx={sideLinkSx}>
        <DescriptionIcon sx={{ fontSize: 16, color: colors.text.secondary, flexShrink: 0 }} />
        <Box component="span" sx={sideLinkLabelSx}>
          {t("app.documentation")}
        </Box>
        <Box component="span" sx={sideLinkArrowSx}>
          ↗
        </Box>
      </Link>
      <Link href={REPO_URL} target="_blank" rel="noopener noreferrer" sx={sideLinkSx}>
        <GitHubIcon sx={{ fontSize: 16, color: colors.text.secondary, flexShrink: 0 }} />
        <Box component="span" sx={sideLinkLabelSx}>
          {REPO}
        </Box>
        <Box component="span" sx={sideLinkArrowSx}>
          ↗
        </Box>
      </Link>
      <Box
        sx={{
          display: "flex",
          alignItems: "center",
          gap: "8px",
          p: "6px 8px",
          fontFamily: fonts.mono,
          fontSize: 11,
          color: colors.text.secondary,
        }}
      >
        <Box
          component="span"
          sx={{
            width: 6,
            height: 6,
            borderRadius: "50%",
            backgroundColor: colors.state.success,
            boxShadow: "0 0 6px rgba(102, 187, 106, 0.7)",
            flexShrink: 0,
          }}
        />
        <Box component="span">{t("app.version", { version: version || "dev" })}</Box>
      </Box>
    </Box>
  );
}
