import { useState } from "react";
import { Box, Link } from "@mui/material";
import { colors, fonts, radiusPx } from "@design";
import { DescriptionIcon, GitHubIcon } from "@b4.icons";
import { UpdateModal } from "./UpdateDialog";
import { useGitHubRelease, dismissVersion } from "@hooks/useGitHubRelease";
import { useTranslation } from "react-i18next";

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
    backgroundColor: "rgba(255, 255, 255, 0.04)",
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

export default function Version() {
  const { t, i18n } = useTranslation();
  const [updateModalOpen, setUpdateModalOpen] = useState(false);
  const {
    releases,
    latestRelease,
    isNewVersionAvailable,
    currentVersion,
    includePrerelease,
    setIncludePrerelease,
  } = useGitHubRelease();

  const docsUrl = i18n.language.startsWith("ru")
    ? `${DOCS_URL}/ru/`
    : `${DOCS_URL}/`;

  const versionStr = currentVersion.replace(/^v/, "");
  const fromTag = currentVersion.startsWith("v")
    ? currentVersion
    : `v${currentVersion}`;
  const toTag = latestRelease?.tag_name ?? "";

  const handleDismiss = () => {
    if (latestRelease) dismissVersion(latestRelease.tag_name);
    setUpdateModalOpen(false);
  };

  return (
    <>
      <Box
        sx={{
          mt: "auto",
          p: "10px 12px",
          display: "flex",
          flexDirection: "column",
          gap: "8px",
        }}
      >
        <Link
          href={docsUrl}
          target="_blank"
          rel="noopener noreferrer"
          sx={sideLinkSx}
        >
          <DescriptionIcon
            sx={{ fontSize: 16, color: colors.text.secondary, flexShrink: 0 }}
          />
          <Box component="span" sx={sideLinkLabelSx}>
            {t("core.nav.documentation")}
          </Box>
          <Box component="span" sx={sideLinkArrowSx}>
            ↗
          </Box>
        </Link>

        <Link
          href={REPO_URL}
          target="_blank"
          rel="noopener noreferrer"
          sx={sideLinkSx}
        >
          <GitHubIcon
            sx={{ fontSize: 16, color: colors.text.secondary, flexShrink: 0 }}
          />
          <Box component="span" sx={sideLinkLabelSx}>
            {REPO}
          </Box>
          <Box component="span" sx={sideLinkArrowSx}>
            ↗
          </Box>
        </Link>

        {isNewVersionAvailable && latestRelease ? (
          <Box
            role="button"
            tabIndex={0}
            aria-haspopup="dialog"
            onClick={() => setUpdateModalOpen(true)}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                setUpdateModalOpen(true);
              }
            }}
            title={t("update.clickToView")}
            sx={{
              display: "flex",
              alignItems: "center",
              gap: "10px",
              p: "8px 10px",
              borderRadius: `${radiusPx.sm}px`,
              backgroundColor: "rgba(245, 173, 24, 0.12)",
              border: "1px solid rgba(245, 173, 24, 0.4)",
              cursor: "pointer",
              transition: "background-color 150ms ease",
              "&:hover": { backgroundColor: "rgba(245, 173, 24, 0.20)" },
              "@keyframes b4UpdatePulse": {
                "0%": { transform: "scale(0.6)", opacity: 0.9 },
                "100%": { transform: "scale(1.7)", opacity: 0 },
              },
            }}
          >
            <Box
              component="span"
              sx={{
                position: "relative",
                width: 8,
                height: 8,
                borderRadius: "50%",
                backgroundColor: colors.secondary,
                flexShrink: 0,
                "&::after": {
                  content: '""',
                  position: "absolute",
                  inset: "-4px",
                  borderRadius: "50%",
                  border: `1.5px solid ${colors.secondary}`,
                  animation: "b4UpdatePulse 1.6s ease-out infinite",
                  opacity: 0,
                },
              }}
            />
            <Box
              sx={{
                display: "flex",
                flexDirection: "column",
                flex: 1,
                minWidth: 0,
                lineHeight: 1.25,
              }}
            >
              <Box
                component="span"
                sx={{
                  fontSize: 11,
                  color: colors.secondary,
                  fontWeight: 700,
                  textTransform: "uppercase",
                  letterSpacing: "0.08em",
                }}
              >
                {t("update.available")}
              </Box>
              <Box
                component="span"
                sx={{
                  fontFamily: fonts.mono,
                  fontSize: 10,
                  color: colors.text.secondary,
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                  whiteSpace: "nowrap",
                }}
              >
                {fromTag} → {toTag}
              </Box>
            </Box>
            <Box
              component="span"
              sx={{
                fontSize: 10,
                color: colors.secondary,
                fontWeight: 700,
                textTransform: "uppercase",
                letterSpacing: "0.06em",
                px: "8px",
                py: "3px",
                border: `1px solid ${colors.secondary}`,
                borderRadius: "3px",
                backgroundColor: "rgba(245, 173, 24, 0.10)",
                flexShrink: 0,
              }}
            >
              {t("update.install")}
            </Box>
          </Box>
        ) : (
          <Box
            role="button"
            tabIndex={0}
            aria-haspopup="dialog"
            onClick={() => setUpdateModalOpen(true)}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                setUpdateModalOpen(true);
              }
            }}
            title={t("update.clickToView")}
            sx={{
              display: "flex",
              alignItems: "center",
              gap: "8px",
              p: "6px 8px",
              borderRadius: `${radiusPx.sm}px`,
              fontFamily: fonts.mono,
              fontSize: 11,
              color: colors.text.secondary,
              cursor: "pointer",
              transition: "background-color 150ms ease",
              "&:hover": {
                backgroundColor: "rgba(255, 255, 255, 0.04)",
              },
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
            <Box
              component="span"
              sx={{
                color: colors.text.disabled,
                textTransform: "uppercase",
                letterSpacing: "0.1em",
                fontSize: 9,
              }}
            >
              v
            </Box>
            <Box
              component="span"
              sx={{ color: colors.text.primary, fontWeight: 600 }}
            >
              {versionStr}
            </Box>
            <Box
              component="span"
              sx={{ color: colors.text.secondary, opacity: 0.7 }}
            >
              · {t("update.upToDate")}
            </Box>
          </Box>
        )}
      </Box>

      <UpdateModal
        open={updateModalOpen}
        onClose={() => setUpdateModalOpen(false)}
        onDismiss={handleDismiss}
        currentVersion={currentVersion}
        releases={releases}
        includePrerelease={includePrerelease}
        onTogglePrerelease={setIncludePrerelease}
      />
    </>
  );
}
