import { useState } from "react";
import { Box, Button, Typography, Paper } from "@mui/material";
import { B4TextField } from "@b4.fields";
import { B4Alert } from "@b4.elements";
import { colors, radiusPx } from "@design";
import { useAuth } from "@context/AuthProvider";
import { Logo } from "@common/Logo";
import { useTranslation } from "react-i18next";

export function LoginPage() {
  const { t } = useTranslation();
  const { login } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    setLoading(true);

    const err = await login(username, password);
    if (err) {
      setError(err);
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

        <form onSubmit={(e) => void handleSubmit(e)}>
          <Box sx={{ display: "flex", flexDirection: "column", gap: 2 }}>
            {error && (
              <B4Alert noWrapper severity="error">
                {error}
              </B4Alert>
            )}
            <B4TextField
              label={t("login.username")}
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoFocus
              autoComplete="username"
            />
            <B4TextField
              label={t("login.password")}
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
            />
            <Button
              type="submit"
              variant="contained"
              fullWidth
              disabled={loading || !username || !password}
              sx={{ mt: 1 }}
            >
              {loading ? t("login.signingIn") : t("login.signIn")}
            </Button>
          </Box>
        </form>
      </Paper>
    </Box>
  );
}
