import { useState } from "react";
import { Box, Button } from "@mui/material";
import { B4TextField } from "@b4.fields";
import { B4Alert } from "@b4.elements";
import { useAuth } from "@context/AuthProvider";
import { useTranslation } from "react-i18next";

interface LoginFormProps {
  onSignedIn?: () => void;
}

export function LoginForm({ onSignedIn }: Readonly<LoginFormProps>) {
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
    setLoading(false);
    if (err) {
      setError(err);
      return;
    }
    onSignedIn?.();
  };

  return (
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
  );
}
