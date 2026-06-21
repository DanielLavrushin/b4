import { Typography } from "@mui/material";
import { colors, fonts } from "@design";
import { useTranslation } from "react-i18next";
import { EscalationEntry } from "./Page";
import { DashboardPanel } from "./DashboardPanel";
import { DataRow } from "./DataRow";
import { DomainLabel } from "./DomainLabel";

interface EscalationsProps {
  escalations: EscalationEntry[];
  total: number;
}

const formatTimeLeft = (expiresAt: string): string => {
  const expiry = new Date(expiresAt).getTime();
  if (!Number.isFinite(expiry)) return "";
  const diffMs = expiry - Date.now();
  if (diffMs <= 0) return "0m";
  const minutes = Math.floor(diffMs / 60000);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  const remMin = minutes % 60;
  return remMin === 0 ? `${hours}h` : `${hours}h ${remMin}m`;
};

export const Escalations = ({ escalations, total }: EscalationsProps) => {
  const { t } = useTranslation();
  if (escalations.length === 0) return null;

  return (
    <DashboardPanel
      eyebrow={t("dashboard.escalations.title")}
      right={
        <Typography
          variant="caption"
          sx={{ color: colors.text.secondary, opacity: 0.7 }}
        >
          {t("dashboard.escalations.totalCount", { count: total })}
        </Typography>
      }
    >
      {escalations.map((e) => (
        <DataRow
          key={e.host}
          hover={false}
          right={
            <>
              <Typography
                variant="caption"
                sx={{
                  color: colors.text.secondary,
                  fontFamily: fonts.mono,
                  whiteSpace: "nowrap",
                }}
                title={t("dashboard.escalations.viaSet")}
              >
                → {e.to_set}
              </Typography>
              {e.hops > 1 && (
                <Typography
                  variant="caption"
                  sx={{
                    color: colors.text.secondary,
                    opacity: 0.6,
                    whiteSpace: "nowrap",
                  }}
                  title={t("dashboard.escalations.hops")}
                >
                  ×{e.hops}
                </Typography>
              )}
              <Typography
                variant="caption"
                sx={{
                  color: colors.text.secondary,
                  opacity: 0.7,
                  fontFamily: fonts.mono,
                  whiteSpace: "nowrap",
                  minWidth: 50,
                  textAlign: "right",
                }}
                title={t("dashboard.escalations.expiresIn")}
              >
                {formatTimeLeft(e.expires_at)}
              </Typography>
            </>
          }
        >
          <DomainLabel value={e.host} />
        </DataRow>
      ))}
    </DashboardPanel>
  );
};
