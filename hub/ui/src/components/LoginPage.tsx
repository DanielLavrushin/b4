import { useState, type FormEvent } from "react";
import { Alert, Box, Button, Paper, TextField, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, Logo, radiusPx } from "@design";
import { useSession } from "@/context/SessionProvider";
import { LanguageMenu } from "./LanguageMenu";

export function LoginPage() {
  const { t } = useTranslation();
  const { login, configured } = useSession();
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setError(null);
    setLoading(true);
    const err = await login(password);
    if (err) {
      if (err.status === 401) setError(t("login.wrong"));
      else if (err.status === 429) setError(t("login.throttled"));
      else if (err.status !== 503) setError(t("app.error", { message: err.message }));
    }
    setLoading(false);
  };

  return (
    <Box
      sx={{
        minHeight: "100vh",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        p: 2,
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
          maxWidth: "100%",
          bgcolor: colors.background.paper,
          border: `1px solid ${colors.border.default}`,
          borderRadius: `${radiusPx.md}px`,
          position: "relative",
        }}
      >
        <Box sx={{ position: "absolute", top: 8, right: 8 }}>
          <LanguageMenu />
        </Box>
        <Box sx={{ textAlign: "center", mb: 3 }}>
          <Box sx={{ display: "inline-block" }}>
            <Logo subtitle={t("app.subtitle")} />
          </Box>
          <Typography variant="body2" sx={{ color: colors.text.secondary, mt: 1 }}>
            {t("login.subtitle")}
          </Typography>
        </Box>
        {!configured ? (
          <Alert severity="warning">{t("login.unconfigured")}</Alert>
        ) : (
          <Box component="form" onSubmit={(e) => void submit(e)} sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
            <TextField
              type="password"
              label={t("login.password")}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoFocus
              autoComplete="current-password"
              fullWidth
              size="small"
            />
            {error && <Alert severity="error">{error}</Alert>}
            <Button type="submit" variant="contained" disabled={loading || password === ""} fullWidth>
              {t("login.submit")}
            </Button>
            <Typography variant="caption" sx={{ color: colors.text.disabled, textAlign: "center" }}>
              {t("login.hint")}
            </Typography>
          </Box>
        )}
      </Paper>
    </Box>
  );
}
