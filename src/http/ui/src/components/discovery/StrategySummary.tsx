import { ReactNode } from "react";
import { Box, Stack, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import { colors, facets as facetColors, typography } from "@design";
import { B4SetConfig } from "@models/config";
import { buildSetFacets } from "@components/sets/facets";
import { describeStrategy, normalizeSet } from "@utils";

interface Row {
  key: string;
  label: string;
  color: string;
  value: string;
  muted?: string;
}

interface StrategySummaryProps {
  set: B4SetConfig;
  preset?: string;
  note?: ReactNode;
  domains?: string[];
  compact?: boolean;
}

export const StrategySummary = ({
  set: rawSet,
  preset,
  note,
  domains,
  compact = false,
}: StrategySummaryProps) => {
  const { t } = useTranslation();
  const set = normalizeSet(rawSet);
  const facets = buildSetFacets(set, undefined, t);
  const rows: Row[] = [];

  const strategyLabel = t("sets.card.f.strategy");
  const facetRow = (key: "split" | "fake", label: string) => {
    const facet = facets.find((f) => f.key === key);
    if (!facet?.active || facet.rows.length === 0) return;
    const headIndex = Math.max(
      0,
      facet.rows.findIndex((r) => r.label === strategyLabel),
    );
    const head = facet.rows[headIndex];
    const rest = facet.rows.filter((_, i) => i !== headIndex);
    const muted = rest
      .map(
        (r) =>
          `${r.label.toLowerCase()} ${r.value}${r.muted ? ` ${r.muted}` : ""}`,
      )
      .join(" · ");
    rows.push({
      key,
      label,
      color: facet.color,
      value: head.value.toLowerCase(),
      muted: muted || undefined,
    });
  };

  facetRow("split", t("discovery.summary.split"));
  facetRow("fake", t("discovery.summary.fake"));

  const targets = set.targets;
  const targetDomains = domains ?? targets?.sni_domains ?? [];
  const targetExtras: string[] = [];
  if (targets?.geosite_categories?.length) {
    targetExtras.push(
      t("discovery.summary.geosite", {
        list: targets.geosite_categories.join(", "),
      }),
    );
  }
  if (targets?.geoip_categories?.length) {
    targetExtras.push(
      t("discovery.summary.geoip", { list: targets.geoip_categories.join(", ") }),
    );
  }
  if (targets?.tls) {
    targetExtras.push(t("discovery.summary.tls", { version: targets.tls }));
  }
  if (targets?.ip_version) {
    targetExtras.push(
      t("discovery.summary.ipv", { version: targets.ip_version }),
    );
  }
  rows.push({
    key: "target",
    label: t("discovery.summary.target"),
    color: facetColors.target,
    value: targetDomains.join(", "),
    muted: targetExtras.join(" · ") || undefined,
  });

  const dns = set.dns;
  const pinned = Object.values(dns?.pins ?? {}).flat();
  let dnsValue = t("discovery.summary.dnsNotNeeded");
  let dnsMuted: string | undefined;
  if (dns?.enabled) {
    dnsValue = dns.doh_url
      ? t("discovery.summary.dnsDoh", { url: dns.doh_url })
      : t("discovery.summary.dnsServer", { server: dns.target_dns });
  }
  if (pinned.length > 0) {
    const pinText = t("discovery.summary.dnsPins", {
      list: [...new Set(pinned)].join(", "),
    });
    if (dns?.enabled) dnsMuted = pinText;
    else dnsValue = pinText;
  }
  rows.push({
    key: "dns",
    label: t("discovery.summary.dns"),
    color: facetColors.dns,
    value: dnsValue,
    muted: dnsMuted,
  });

  return (
    <Box>
      <Typography
        component="div"
        sx={{
          fontWeight: 600,
          color: colors.text.primary,
          fontSize: compact ? "0.9rem" : "0.95rem",
          lineHeight: 1.4,
        }}
      >
        {describeStrategy(set, t)}
      </Typography>
      {(preset || note) && (
        <Typography
          component="div"
          sx={{
            ...typography.recipes.monoSmall,
            color: colors.text.disabled,
            mt: "3px",
          }}
        >
          {preset}
          {preset && note ? " · " : ""}
          {note}
        </Typography>
      )}
      <Stack spacing={0.25} sx={{ mt: 1.25 }}>
        {rows.map((row) => (
          <Box
            key={row.key}
            sx={{
              display: "grid",
              gridTemplateColumns: "88px 1fr",
              gap: 1.25,
              alignItems: "baseline",
              minHeight: 20,
            }}
          >
            <Typography
              component="span"
              sx={{
                ...typography.recipes.metricLabel,
                fontWeight: typography.weights.bold,
                color: colors.text.disabled,
                display: "flex",
                alignItems: "center",
                gap: 0.75,
              }}
            >
              <Box
                component="span"
                sx={{
                  width: 8,
                  height: 8,
                  borderRadius: "50%",
                  bgcolor: row.color,
                  flexShrink: 0,
                }}
              />
              {row.label}
            </Typography>
            <Typography
              component="span"
              sx={{
                ...typography.recipes.monoSmall,
                fontSize: typography.sizes.sm,
                color: colors.text.primary,
                overflowWrap: "anywhere",
              }}
            >
              {row.value}
              {row.muted && (
                <Box
                  component="span"
                  sx={{ color: colors.text.disabled, ml: 0.75 }}
                >
                  {row.muted}
                </Box>
              )}
            </Typography>
          </Box>
        ))}
      </Stack>
    </Box>
  );
};
