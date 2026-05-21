import { Grid } from "@mui/material";
import { B4SetConfig, MSSClampConfig, QueueConfig } from "@models/config";
import {
  B4Slider,
  B4RangeSlider,
  B4TextField,
  B4Alert,
  B4FormHeader,
  B4Hint,
} from "@b4.elements";
import { B4Switch } from "@common/B4Switch";
import { useTranslation } from "react-i18next";

interface TcpGeneralProps {
  config: B4SetConfig;
  queue: QueueConfig;
  onChange: (
    field: string,
    value: string | number | boolean | number[],
  ) => void;
}

export const TcpGeneral = ({ config, queue, onChange }: TcpGeneralProps) => {
  const { t } = useTranslation();
  const dup = config.tcp.duplicate ?? { enabled: false, count: 3 };
  const ibd = config.tcp.ip_block_detect ?? {
    enabled: false,
    retransmit_threshold: 3,
    timeout_ms: 3000,
    cache_blocked_ips: true,
  };
  const rstProt = {
    enabled: false,
    ttl_tolerance: 3,
    ...config.tcp.rst_protection,
  };
  const mss: MSSClampConfig = config.mss_clamp ?? { enabled: false, size: 88 };
  const hasIPScope =
    (config.targets?.ip?.length ?? 0) > 0 ||
    (config.targets?.geoip_categories?.length ?? 0) > 0;
  const hasMACScope = (config.targets?.source_devices?.length ?? 0) > 0;
  const mssScopeOk = hasIPScope || hasMACScope;

  return (
    <>
      <Grid container spacing={3} sx={{ mt: 1, mb: 3 }}>
        <Grid size={{ xs: 12, md: 6 }}>
          <B4Slider
            label={t("sets.tcp.general.connPacketsLimit")}
            value={config.tcp.conn_bytes_limit}
            onChange={(value: number) =>
              onChange("tcp.conn_bytes_limit", value)
            }
            min={1}
            max={queue.tcp_conn_bytes_limit}
            step={1}
            helperText={t("sets.tcp.general.connPacketsMax", {
              max: queue.tcp_conn_bytes_limit,
            })}
            aiTopic="tcp.conn_bytes_limit"
            aiContext={{ queue_max: queue.tcp_conn_bytes_limit }}
          />
        </Grid>
        <Grid size={{ xs: 12, md: 6 }}>
          <B4RangeSlider
            label={t("sets.tcp.general.seg2delay")}
            value={[
              config.tcp.seg2delay,
              config.tcp.seg2delay_max || config.tcp.seg2delay,
            ]}
            onChange={(value: [number, number]) => {
              onChange("tcp.seg2delay", value[0]);
              onChange("tcp.seg2delay_max", value[1]);
            }}
            min={0}
            max={1000}
            step={10}
            valueSuffix=" ms"
            helperText={t("sets.tcp.general.seg2delayHelper")}
            aiTopic="tcp.seg2delay"
          />
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <B4TextField
            label={t("sets.tcp.general.portFilter")}
            value={config.tcp.dport_filter}
            onChange={(e) => onChange("tcp.dport_filter", e.target.value)}
            placeholder={t("sets.tcp.general.portFilterPlaceholder")}
            helperText={t("sets.tcp.general.portFilterHelper")}
            aiTopic="tcp.dport_filter"
          />
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <B4Switch
            label={t("sets.tcp.general.dropSack")}
            description={t("sets.tcp.general.dropSackDesc")}
            checked={config.tcp.drop_sack || false}
            onChange={(checked) => onChange("tcp.drop_sack", checked)}
            aiTopic="tcp.drop_sack"
          />
        </Grid>
      </Grid>

      {/* Packet Duplication */}
      <B4FormHeader label={t("sets.tcp.general.packetDuplication")} />
      <Grid container spacing={3} mb={3} mt={2}>
        <Grid size={{ xs: 12, md: 4 }}>
          <B4Switch
            label={t("sets.tcp.general.dupEnable")}
            description={t("sets.tcp.general.dupEnableDesc")}
            checked={dup.enabled}
            onChange={(checked) => onChange("tcp.duplicate.enabled", checked)}
            aiTopic="tcp.duplicate"
            aiContext={{ count: dup.count }}
          />
        </Grid>
        <Grid size={{ xs: 12, md: 8 }}>
          <B4Hint sx={{ my: 1 }}>{t("sets.tcp.general.dupAlert")}</B4Hint>
        </Grid>
        {dup.enabled && (
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Slider
              label={t("sets.tcp.general.dupCount")}
              value={dup.count}
              onChange={(value: number) =>
                onChange("tcp.duplicate.count", value)
              }
              min={1}
              max={10}
              step={1}
              helperText={t("sets.tcp.general.dupCountHelper")}
            />
          </Grid>
        )}
      </Grid>

      {/* IP Block Detection */}
      <B4FormHeader label={t("sets.tcp.general.ipBlockDetect")} />
      <Grid container spacing={3} mb={3} mt={2}>
        <Grid size={{ xs: 12, md: 4 }}>
          <B4Switch
            label={t("sets.tcp.general.ibdEnable")}
            description={t("sets.tcp.general.ibdEnableDesc")}
            checked={ibd.enabled}
            onChange={(checked) =>
              onChange("tcp.ip_block_detect.enabled", checked)
            }
            aiTopic="tcp.ip_block_detect"
            aiContext={{
              retransmit_threshold: ibd.retransmit_threshold,
              timeout_ms: ibd.timeout_ms,
              cache_blocked_ips: ibd.cache_blocked_ips,
            }}
          />
        </Grid>
        <Grid size={{ xs: 12, md: 8 }}>
          <B4Hint sx={{ my: 1 }}>{t("sets.tcp.general.ibdAlert")}</B4Hint>
        </Grid>
        {ibd.enabled && (
          <>
            <Grid size={{ xs: 12, md: 6 }}>
              <B4Slider
                label={t("sets.tcp.general.ibdThreshold")}
                value={ibd.retransmit_threshold}
                onChange={(value: number) =>
                  onChange("tcp.ip_block_detect.retransmit_threshold", value)
                }
                min={1}
                max={10}
                step={1}
                helperText={t("sets.tcp.general.ibdThresholdHelper")}
                aiTopic="tcp.ip_block_detect.retransmit_threshold"
              />
              {ibd.retransmit_threshold <= 1 && (
                <B4Alert severity="warning">
                  {t("sets.tcp.general.ibdThresholdWarn")}
                </B4Alert>
              )}
            </Grid>

            <Grid size={{ xs: 12, md: 6 }}>
              <B4Slider
                label={t("sets.tcp.general.ibdTimeout")}
                value={ibd.timeout_ms}
                onChange={(value: number) =>
                  onChange("tcp.ip_block_detect.timeout_ms", value)
                }
                min={1000}
                max={10000}
                step={500}
                valueSuffix=" ms"
                helperText={t("sets.tcp.general.ibdTimeoutHelper")}
              />
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <B4Switch
                label={t("sets.tcp.general.ibdCache")}
                description={t("sets.tcp.general.ibdCacheDesc")}
                checked={ibd.cache_blocked_ips}
                onChange={(checked) =>
                  onChange("tcp.ip_block_detect.cache_blocked_ips", checked)
                }
              />
            </Grid>
          </>
        )}
      </Grid>

      {/* RST Protection */}
      <B4FormHeader label={t("sets.tcp.general.rstProtection")} />
      <Grid container spacing={3} mt={2}>
        <Grid size={{ xs: 12, md: 4 }}>
          <B4Switch
            label={t("sets.tcp.general.rstEnable")}
            description={t("sets.tcp.general.rstEnableDesc")}
            checked={rstProt.enabled}
            onChange={(checked) =>
              onChange("tcp.rst_protection.enabled", checked)
            }
            aiTopic="tcp.rst_protection"
            aiContext={{ ttl_tolerance: rstProt.ttl_tolerance }}
          />
        </Grid>
        <Grid size={{ xs: 12, md: 8 }}>
          <B4Hint sx={{ mt: 1 }}>{t("sets.tcp.general.rstAlert")}</B4Hint>
        </Grid>

        {rstProt.enabled && (
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Slider
              label={t("sets.tcp.general.rstTtlTolerance")}
              value={rstProt.ttl_tolerance}
              onChange={(value: number) =>
                onChange("tcp.rst_protection.ttl_tolerance", value)
              }
              min={1}
              max={20}
              step={1}
              helperText={t("sets.tcp.general.rstTtlToleranceHelper")}
              aiTopic="tcp.rst_protection.ttl_tolerance"
            />
          </Grid>
        )}
      </Grid>

      <B4FormHeader label={t("sets.tcp.general.mssClamp")} />
      <Grid container spacing={3} mt={2}>
        <Grid size={{ xs: 12, md: 4 }}>
          <B4Switch
            label={t("sets.tcp.general.mssEnable")}
            description={t("sets.tcp.general.mssEnableDesc")}
            checked={mss.enabled}
            onChange={(checked) => {
              onChange("mss_clamp.enabled", checked);
              if (checked && (!mss.size || mss.size < 10)) {
                onChange("mss_clamp.size", 88);
              }
            }}
            disabled={!mssScopeOk}
            aiTopic="mss_clamp"
            aiContext={{ size: mss.size }}
          />
        </Grid>
        <Grid size={{ xs: 12, md: 8 }}>
          <B4Hint sx={{ mt: 1 }}>{t("sets.tcp.general.mssScopeHint")}</B4Hint>
          {!mssScopeOk && (
            <B4Alert severity="warning">
              {t("sets.tcp.general.mssScopeRequired")}
            </B4Alert>
          )}
          {mss.enabled && mssScopeOk && (
            <B4Alert severity="info">
              {t("sets.tcp.general.mssAppliesTo", {
                ips: hasIPScope ? "✓" : "—",
                macs: hasMACScope ? "✓" : "—",
              })}
            </B4Alert>
          )}
        </Grid>
        {mss.enabled && mssScopeOk && (
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Slider
              label={t("sets.tcp.general.mssSize")}
              value={mss.size}
              onChange={(value: number) => onChange("mss_clamp.size", value)}
              min={10}
              max={1460}
              step={1}
              helperText={t("sets.tcp.general.mssSizeHelper")}
              aiTopic="mss_clamp.size"
            />
          </Grid>
        )}
      </Grid>
    </>
  );
};
