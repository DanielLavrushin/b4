import { SxProps, Theme, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import type { TFunction } from "i18next";
import { B4Alert } from "@b4.elements";
import { colors, typography } from "@design";
import {
  AsnView,
  formatAsn,
  formatCompactCount,
  isAsnResolved,
  isLargeNetwork,
} from "@models/asn";
import { formatTimeAgo } from "@utils";

export function asnUpdatedText(t: TFunction, view: AsnView): string {
  if (!view.updated_at) return t("core.asn.neverFetched");
  const ago = formatTimeAgo(t, new Date(view.updated_at * 1000).toISOString());
  return ago ? t("core.asn.updated", { ago }) : t("core.asn.neverFetched");
}

export function asnFactParts(
  t: TFunction,
  view: AsnView,
  lng?: string,
  withUpdated = true,
): string[] {
  if (!isAsnResolved(view)) return [];
  const parts = [
    t("core.asn.prefixes", {
      total: view.prefix_count.toLocaleString(lng),
      v4: view.v4_count.toLocaleString(lng),
      v6: view.v6_count.toLocaleString(lng),
    }),
  ];
  if (view.v4_count > 0) {
    parts.push(
      t("core.asn.ipv4Addresses", {
        value: formatCompactCount(view.ipv4_addresses, lng),
      }),
    );
  }
  if (withUpdated) parts.push(asnUpdatedText(t, view));
  return parts;
}

interface AsnFactsProps {
  view: AsnView;
  withUpdated?: boolean;
  sx?: SxProps<Theme>;
}

export const AsnFacts = ({ view, withUpdated = true, sx }: AsnFactsProps) => {
  const { t, i18n } = useTranslation();
  const parts = asnFactParts(t, view, i18n.language, withUpdated);
  if (parts.length === 0) return null;
  return (
    <Typography
      component="div"
      sx={{
        ...typography.recipes.monoSmall,
        color: colors.text.secondary,
        ...sx,
      }}
    >
      {parts.join(" · ")}
    </Typography>
  );
};

interface AsnLargeNetworkAlertProps {
  view?: AsnView | null;
  sx?: SxProps<Theme>;
}

export const AsnLargeNetworkAlert = ({
  view,
  sx,
}: AsnLargeNetworkAlertProps) => {
  const { t, i18n } = useTranslation();
  if (!view || !isLargeNetwork(view)) return null;
  return (
    <B4Alert severity="warning" noWrapper sx={sx}>
      {t("core.asn.largeNetwork", {
        asn: formatAsn(view.id),
        prefixes: view.prefix_count.toLocaleString(i18n.language),
        addresses: formatCompactCount(view.ipv4_addresses, i18n.language),
      })}
    </B4Alert>
  );
};
