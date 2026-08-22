import { Box, Grid, Stack } from "@mui/material";
import { EscalateIcon } from "@b4.icons";
import {
  B4Alert,
  B4FormHeader,
  B4Hint,
  B4Section,
  B4Select,
  B4Slider,
} from "@b4.elements";
import { B4SetConfig } from "@models/config";
import { useTranslation } from "react-i18next";

export const wouldCreateEscalationCycle = (
  candidate: B4SetConfig,
  currentId: string,
  all: B4SetConfig[],
): boolean => {
  const byId = new Map(all.map((s) => [s.id, s]));
  const seen = new Set<string>();
  let cur: B4SetConfig | undefined = candidate;
  while (cur?.escalate?.to) {
    if (cur.escalate.to === currentId) return true;
    if (seen.has(cur.escalate.to)) return false;
    seen.add(cur.escalate.to);
    cur = byId.get(cur.escalate.to);
  }
  return false;
};

interface EscalationSettingsProps {
  config: B4SetConfig;
  allSets: B4SetConfig[];
  onChange: (
    field: string,
    value:
      | string
      | number
      | boolean
      | string[]
      | number[]
      | Record<string, string[]>
      | null
      | undefined,
  ) => void;
}

export const EscalationSettings = ({
  config,
  allSets,
  onChange,
}: EscalationSettingsProps) => {
  const { t } = useTranslation();
  const escalateOn = !!config.escalate?.to;
  const rstOn = config.tcp.rst_protection?.enabled === true;
  const showRstWarn = escalateOn && !rstOn;

  return (
    <Stack spacing={3}>
      <B4Section
        title={t("sets.escalation.sectionTitle")}
        description={t("sets.escalation.sectionDescription")}
        icon={<EscalateIcon />}
      >
        <B4FormHeader label={t("sets.escalation.target")} />
        <Grid container spacing={3}>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Select
              label={t("sets.targets.escalateTo")}
              value={config.escalate?.to ?? ""}
              options={[
                { value: "", label: t("sets.targets.escalateNone") },
                ...allSets
                  .filter(
                    (s) =>
                      s.id &&
                      s.id !== config.id &&
                      !wouldCreateEscalationCycle(s, config.id, allSets),
                  )
                  .map((s) => ({
                    label: s.enabled
                      ? s.name || s.id
                      : `${s.name || s.id} (${t("sets.escalation.targetDisabled")})`,
                    value: s.id,
                  })),
              ]}
              onChange={(e) => onChange("escalate.to", e.target.value)}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Hint sx={{ mt: 1 }}>{t("sets.targets.escalateHelper")}</B4Hint>
          </Grid>
        </Grid>

        {showRstWarn && (
          <Box>
            <B4Alert severity="warning" noWrapper>
              {t("sets.editor.escalateNeedsRstProtection")}
            </B4Alert>
          </Box>
        )}

        {escalateOn && (
          <>
            <B4FormHeader label={t("sets.escalation.triggers")} />
            <B4Hint sx={{ mb: 1 }}>{t("sets.escalation.triggersHelper")}</B4Hint>
            <Grid container spacing={3}>
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Slider
                  label={t("sets.escalation.stallThreshold")}
                  value={config.escalate?.stall_threshold || 3}
                  onChange={(value: number) =>
                    onChange("escalate.stall_threshold", value)
                  }
                  min={1}
                  max={20}
                  step={1}
                  helperText={t("sets.escalation.stallThresholdHelper")}
                />
              </Grid>
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Slider
                  label={t("sets.escalation.stallTimeoutMs")}
                  value={config.escalate?.stall_timeout_ms || 3000}
                  onChange={(value: number) =>
                    onChange("escalate.stall_timeout_ms", value)
                  }
                  min={500}
                  max={15000}
                  step={500}
                  valueSuffix=" ms"
                  helperText={t("sets.escalation.stallTimeoutHelper")}
                />
              </Grid>
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Slider
                  label={t("sets.escalation.rstThreshold")}
                  value={config.escalate?.rst_threshold || 3}
                  onChange={(value: number) =>
                    onChange("escalate.rst_threshold", value)
                  }
                  min={1}
                  max={20}
                  step={1}
                  helperText={t("sets.escalation.rstThresholdHelper")}
                />
              </Grid>
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Slider
                  label={t("sets.escalation.rstWindowSec")}
                  value={config.escalate?.rst_window_sec || 30}
                  onChange={(value: number) =>
                    onChange("escalate.rst_window_sec", value)
                  }
                  min={5}
                  max={600}
                  step={5}
                  valueSuffix=" s"
                  helperText={t("sets.escalation.rstWindowHelper")}
                />
              </Grid>
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Slider
                  label={t("sets.escalation.dnsThreshold")}
                  value={config.escalate?.dns_threshold || 2}
                  onChange={(value: number) =>
                    onChange("escalate.dns_threshold", value)
                  }
                  min={1}
                  max={10}
                  step={1}
                  helperText={t("sets.escalation.dnsThresholdHelper")}
                />
              </Grid>
            </Grid>

            <B4FormHeader label={t("sets.escalation.tuning")} />
            <Grid container spacing={3}>
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Slider
                  label={t("sets.escalation.ttlMin")}
                  value={Math.round((config.escalate?.ttl_sec || 3600) / 60)}
                  onChange={(value: number) =>
                    onChange("escalate.ttl_sec", value * 60)
                  }
                  min={5}
                  max={1440}
                  step={15}
                  valueSuffix=" min"
                  helperText={t("sets.escalation.ttlHelper")}
                />
              </Grid>
            </Grid>
          </>
        )}
      </B4Section>
    </Stack>
  );
};
