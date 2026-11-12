import { Box } from "@mui/material";
import { LockOutlined as LockIcon } from "@mui/icons-material";
import { useTranslation } from "react-i18next";
import { apiClient } from "@api/apiClient";
import { B4Dialog } from "@b4.elements";
import { useAuth } from "@context/AuthProvider";
import { retryMetrics } from "@/stores/useMetrics";
import { LoginForm } from "./LoginForm";

const stayOpen = () => {};

const ABOVE_PAGE_BACKDROPS = 10000;

const resumeAfterSignIn = () => {
  retryMetrics();
  void apiClient.invalidateQueries();
};

export function SessionLostDialog() {
  const { t } = useTranslation();
  const { sessionLost } = useAuth();

  return (
    <B4Dialog
      open={sessionLost}
      onClose={stayOpen}
      title={t("login.sessionExpired")}
      subtitle={t("login.sessionExpiredHint")}
      icon={<LockIcon />}
      sx={{ zIndex: ABOVE_PAGE_BACKDROPS }}
    >
      <Box sx={{ pt: 1, width: { sm: 340 } }}>
        {sessionLost && <LoginForm onSignedIn={resumeAfterSignIn} />}
      </Box>
    </B4Dialog>
  );
}
