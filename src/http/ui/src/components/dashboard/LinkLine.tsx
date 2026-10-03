import { memo, type ReactNode } from "react";
import { Box, Button } from "@mui/material";
import {
  CloudOff as OfflineIcon,
  HourglassEmpty as WaitingIcon,
  LockOutlined as LockIcon,
  SyncProblem as PollingIcon,
} from "@mui/icons-material";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { colors, radiusPx } from "@design";
import { formatClock } from "@common/charts";
import { useAuth } from "@context/AuthProvider";
import {
  retryMetrics,
  useClientNow,
  useMetrics,
  useMetricsLink,
  type MetricsLink,
} from "@/stores/useMetrics";
import { useStaleSince } from "./panels";
import { srOnlySx } from "./panels/styles";

const SLOW_WAIT_MS = 5_000;
const POLL_SECONDS = 5;

const iconSx = {
  display: "inline-flex",
  color: colors.text.secondary,
  "& svg": { fontSize: 18 },
} as const;

const actionSx = {
  py: "2px",
  px: "10px",
  minWidth: 0,
  fontSize: 12,
  fontWeight: 600,
  textTransform: "none",
  whiteSpace: "nowrap",
} as const;

function RetryButton() {
  const { t } = useTranslation();
  return (
    <Button size="small" variant="outlined" onClick={retryMetrics} sx={actionSx}>
      {t("metricsLink.retry")}
    </Button>
  );
}

const reloadPage = () => window.location.reload();

function SignInButton() {
  const { t } = useTranslation();
  const { authRequired, logout } = useAuth();
  return (
    <Button
      size="small"
      variant="contained"
      onClick={authRequired ? logout : reloadPage}
      sx={actionSx}
    >
      {t("metricsLink.signIn")}
    </Button>
  );
}

function retryMessage(
  t: TFunction,
  locale: string,
  seconds: number,
  receivedAt: number,
): string {
  if (seconds <= 0) return t("metricsLink.retryingNow");
  if (receivedAt > 0) {
    return t("metricsLink.down", {
      time: formatClock(receivedAt, locale, true),
      seconds,
    });
  }
  return t("metricsLink.downNever", { seconds });
}

function RetryText({ retryAt, withTime }: { retryAt: number; withTime: boolean }) {
  const { t, i18n } = useTranslation();
  const now = useClientNow(1000);
  const receivedAt = useMetrics((s) => (withTime ? s.receivedAt : 0));
  return (
    <>
      {retryMessage(t, i18n.language, Math.ceil((retryAt - now) / 1000), receivedAt)}
    </>
  );
}

function WaitingText({ since }: { since: number }) {
  const { t } = useTranslation();
  const now = useClientNow(1000);
  const elapsed = now - since;
  if (elapsed < SLOW_WAIT_MS) return <>{t("metricsLink.waiting")}</>;
  return (
    <>
      {t("metricsLink.waitingSlow", {
        seconds: Math.max(1, Math.floor(elapsed / 1000)),
      })}
    </>
  );
}

interface LineView {
  icon: ReactNode;
  text: ReactNode;
  announce: string;
  action?: ReactNode;
  large?: boolean;
}

function Line({ icon, text, action, large = false }: LineView) {
  return (
    <Box
      sx={{
        display: "flex",
        flexWrap: "wrap",
        alignItems: "center",
        columnGap: "10px",
        rowGap: "8px",
        mb: 1.5,
        px: "14px",
        py: large ? "14px" : "7px",
        bgcolor: large ? colors.background.paper : colors.background.control,
        border: `1px solid ${large ? colors.border.default : colors.border.light}`,
        borderRadius: `${radiusPx.md}px`,
        color: colors.text.secondary,
        fontSize: large ? 14 : 13,
        lineHeight: 1.45,
      }}
    >
      <Box component="span" aria-hidden sx={iconSx}>
        {icon}
      </Box>
      <Box
        component="span"
        aria-hidden
        sx={{ flex: "1 1 240px", minWidth: 0, fontVariantNumeric: "tabular-nums" }}
      >
        {text}
      </Box>
      {action}
    </Box>
  );
}

function useLineView(): LineView | null {
  const { t, i18n } = useTranslation();
  const locale = i18n.language;
  const link: MetricsLink = useMetricsLink();
  const hasFrame = useMetrics((s) => s.frame !== null);
  const staleSince = useStaleSince();

  if (link.state === "auth") {
    return {
      icon: <LockIcon />,
      text: t("metricsLink.auth"),
      announce: t("metricsLink.auth"),
      action: <SignInButton />,
      large: !hasFrame,
    };
  }
  if (link.state === "retrying") {
    return {
      icon: <OfflineIcon />,
      text: <RetryText retryAt={link.retryAt} withTime={hasFrame} />,
      announce: retryMessage(
        t,
        locale,
        Math.ceil((link.retryAt - link.since) / 1000),
        hasFrame ? staleSince : 0,
      ),
      action: <RetryButton />,
      large: !hasFrame,
    };
  }
  if (!hasFrame) {
    return {
      icon: <WaitingIcon />,
      text: <WaitingText since={link.since} />,
      announce: t("metricsLink.waiting"),
      action: <RetryButton />,
      large: true,
    };
  }
  if (link.state === "polling") {
    const text = t("metricsLink.polling", { seconds: POLL_SECONDS });
    return { icon: <PollingIcon />, text, announce: text, action: <RetryButton /> };
  }
  if (staleSince <= 0) return null;
  if (link.state === "connecting") {
    const text = t("metricsLink.connecting");
    return { icon: <WaitingIcon />, text, announce: text };
  }
  const text = t("metricsLink.stale", { time: formatClock(staleSince, locale, true) });
  return { icon: <OfflineIcon />, text, announce: text, action: <RetryButton /> };
}

export const LinkLine = memo(function LinkLine() {
  const view = useLineView();
  return (
    <>
      <Box component="span" role="status" sx={srOnlySx}>
        {view?.announce ?? ""}
      </Box>
      {view && <Line {...view} />}
    </>
  );
});
