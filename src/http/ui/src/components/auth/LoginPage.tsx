import { Box, Typography, Paper } from "@mui/material";
import { colors, radiusPx } from "@design";
import { Logo } from "@common/Logo";
import { useTranslation } from "react-i18next";
import { LoginForm } from "./LoginForm";

export function LoginPage() {
  const { t } = useTranslation();

  return (
    <Box
      sx={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        background: `radial-gradient(ellipse 80% 60% at 22% 80%, rgba(245, 173, 24, 0.10) 0%, transparent 60%),
                     radial-gradient(ellipse 60% 50% at 78% 22%, rgba(158, 28, 96, 0.18) 0%, transparent 65%),
                     ${colors.background.default}`,
      }}
    >
      <Paper
        elevation={0}
        sx={{
          p: 4,
          width: 380,
          bgcolor: colors.background.paper,
          border: `1px solid ${colors.border.default}`,
          borderRadius: `${radiusPx.md}px`,
        }}
      >
        <Box sx={{ textAlign: "center", mb: 3 }}>
          <Box sx={{ display: "inline-block" }}>
            <Logo />
          </Box>
          <Typography
            variant="body2"
            sx={{ color: colors.text.secondary, mt: 1 }}
          >
            {t("login.subtitle")}
          </Typography>
        </Box>

        <LoginForm />
      </Paper>
    </Box>
  );
}
