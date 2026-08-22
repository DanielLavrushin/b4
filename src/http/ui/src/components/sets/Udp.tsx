import { useEffect } from "react";
import { Grid } from "@mui/material";
import { Link } from "react-router";
import { DnsIcon, WarningIcon } from "@b4.icons";
import {
  B4Slider,
  B4RangeSlider,
  B4Switch,
  B4Select,
  B4TextField,
  B4Section,
  B4Alert,
  B4FormHeader,
  B4Hint,
} from "@b4.elements";
import {
  B4SetConfig,
  QueueConfig,
  UdpMode,
  UDP_FAKE_PAYLOAD_AUTO_QUIC,
  UDP_FAKE_PAYLOAD_PRESET_1,
  UDP_FAKE_PAYLOAD_PRESET_2,
} from "@models/config";
import { useCaptures } from "@b4.capture";
import { useTranslation, Trans } from "react-i18next";

interface UdpSettingsProps {
  config: B4SetConfig;
  queue: QueueConfig;
  onChange: (field: string, value: string | boolean | number) => void;
}

export const UdpSettings = ({ config, queue, onChange }: UdpSettingsProps) => {
  const { t } = useTranslation();
  const { captures, loadCaptures } = useCaptures();

  useEffect(() => {
    loadCaptures().catch(() => {});
  }, [loadCaptures]);

  const UDP_MODES = [
    {
      value: "drop",
      label: t("sets.udp.modeDrop"),
      description: t("sets.udp.modeDropDesc"),
    },
    {
      value: "reject",
      label: t("sets.udp.modeReject"),
      description: t("sets.udp.modeRejectDesc"),
    },
    {
      value: "fake",
      label: t("sets.udp.modeFake"),
      description: t("sets.udp.modeFakeDesc"),
    },
  ];

  const UDP_QUIC_FILTERS = [
    {
      value: "disabled",
      label: t("sets.udp.quicDisabled"),
      description: t("sets.udp.quicDisabledDesc"),
    },
    {
      value: "all",
      label: t("sets.udp.quicAll"),
      description: t("sets.udp.quicAllDesc"),
    },
    {
      value: "parse",
      label: t("sets.udp.quicParse"),
      description: t("sets.udp.quicParseDesc"),
    },
  ];

  const UDP_FAKING_STRATEGIES = [
    {
      value: "none",
      label: t("sets.udp.strategyNone"),
      description: t("sets.udp.strategyNoneDesc"),
    },
    {
      value: "ttl",
      label: t("sets.udp.strategyTtl"),
      description: t("sets.udp.strategyTtlDesc"),
    },
    {
      value: "checksum",
      label: t("sets.udp.strategyChecksum"),
      description: t("sets.udp.strategyChecksumDesc"),
    },
  ];

  const isQuicEnabled = config.udp.filter_quic !== "disabled";
  const hasPortFilter =
    config.udp.dport_filter && config.udp.dport_filter.trim() !== "";
  const hasDomainsConfigured =
    config.targets?.sni_domains?.length > 0 ||
    config.targets?.geosite_categories?.length > 0;

  const willProcessUdp = isQuicEnabled || hasPortFilter;

  const showActionSettings = willProcessUdp;

  const isFakeMode = config.udp.mode === "fake";
  const showFakeSettings = showActionSettings && isFakeMode;
  const isAutoQuic =
    config.udp.fake_payload_file === UDP_FAKE_PAYLOAD_AUTO_QUIC;

  let payloadFileHelperKey: string;
  if (isAutoQuic) {
    payloadFileHelperKey = "sets.udp.fakePayloadFileAutoQuic";
  } else if (captures.length === 0) {
    payloadFileHelperKey = "sets.udp.fakePayloadFileEmpty";
  } else {
    payloadFileHelperKey = "sets.udp.fakePayloadFileHelper";
  }

  const captureProtocolRank = (proto: string) => (proto === "quic" ? 0 : 1);
  const sortedCaptures = [...captures].sort(
    (a, b) => captureProtocolRank(a.protocol) - captureProtocolRank(b.protocol),
  );

  const showParseWarning =
    config.udp.filter_quic === "parse" && !hasDomainsConfigured;
  const showNoProcessingWarning = !willProcessUdp;

  return (
    <B4Section
      title={t("sets.udp.sectionTitle")}
      description={t("sets.udp.sectionDescription")}
      icon={<DnsIcon />}
    >
      <Grid container spacing={3}>
        <B4FormHeader label={t("sets.udp.trafficHeader")} />

        <Grid size={{ xs: 12, md: 6 }}>
          <B4Select
            label={t("sets.udp.quicFilter")}
            value={config.udp.filter_quic}
            options={UDP_QUIC_FILTERS}
            onChange={(e) => onChange("udp.filter_quic", e.target.value)}
            helperText={
              UDP_QUIC_FILTERS.find((o) => o.value === config.udp.filter_quic)
                ?.description
            }
          />
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <B4TextField
            label={t("sets.udp.portFilter")}
            value={config.udp.dport_filter}
            onChange={(e) => onChange("udp.dport_filter", e.target.value)}
            placeholder={t("sets.udp.portFilterPlaceholder")}
            helperText={t("sets.udp.portFilterHelper")}
          />
        </Grid>

        {/* STUN Filter */}
        <Grid size={{ xs: 12, md: 6 }}>
          <B4Switch
            label={t("sets.udp.filterStun")}
            checked={config.udp.filter_stun}
            onChange={(checked) => onChange("udp.filter_stun", checked)}
            description={t("sets.udp.filterStunDesc")}
          />
        </Grid>

        {willProcessUdp && (
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Slider
              label={t("sets.udp.connPacketsLimit")}
              value={config.udp.conn_bytes_limit}
              onChange={(value) => onChange("udp.conn_bytes_limit", value)}
              min={1}
              max={queue.udp_conn_bytes_limit}
              step={1}
              helperText={t("sets.udp.connPacketsMax", {
                max: queue.udp_conn_bytes_limit,
              })}
            />
          </Grid>
        )}

        {/* Parse mode warning */}
        {showParseWarning && (
          <B4Alert severity="warning" icon={<WarningIcon />}>
            <Trans i18nKey="sets.udp.parseWarning" />
          </B4Alert>
        )}

        {/* No processing warning */}
        {showNoProcessingWarning && (
          <B4Alert>
            <Trans i18nKey="sets.udp.noProcessingWarning" />
          </B4Alert>
        )}

        {/* Section 2: Action Settings (only if traffic will be processed) */}
        {showActionSettings && (
          <>
            <B4FormHeader label={t("sets.udp.actionHeader")} />

            {/* UDP Mode */}
            <Grid size={{ xs: 12, md: 6 }}>
              <B4Select
                label={t("sets.udp.actionMode")}
                value={config.udp.mode}
                options={UDP_MODES}
                onChange={(e) => onChange("udp.mode", e.target.value)}
                helperText={
                  UDP_MODES.find((o) => o.value === config.udp.mode)
                    ?.description
                }
              />
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              {/* Info about current mode */}
              <B4Hint>
                {(() => {
                  const infoKeys: Record<UdpMode, string> = {
                    fake: "sets.udp.fakeModeInfo",
                    reject: "sets.udp.rejectModeInfo",
                    drop: "sets.udp.dropModeInfo",
                  };
                  return (
                    <Trans
                      i18nKey={infoKeys[config.udp.mode] || infoKeys.drop}
                    />
                  );
                })()}
              </B4Hint>
            </Grid>
          </>
        )}

        {/* Section 3: Fake Mode Settings (only if fake mode is enabled) */}
        {showFakeSettings && (
          <>
            <B4FormHeader label={t("sets.udp.fakeHeader")} />

            <Grid size={{ xs: 12, md: 6 }}>
              <B4Slider
                label={t("sets.udp.fakeCount")}
                value={config.udp.fake_seq_length}
                onChange={(value) => onChange("udp.fake_seq_length", value)}
                min={1}
                max={20}
                step={1}
                helperText={t("sets.udp.fakeCountHelper")}
              />
            </Grid>

            <Grid size={{ xs: 12, md: 6 }}>
              <B4Slider
                label={t("sets.udp.fakeSize")}
                value={config.udp.fake_len}
                onChange={(value) => onChange("udp.fake_len", value)}
                min={32}
                max={1500}
                step={8}
                valueSuffix=" bytes"
                helperText={t("sets.udp.fakeSizeHelper")}
              />
            </Grid>

            <Grid size={{ xs: 12, md: 6 }}>
              <B4Select
                label={t("sets.udp.fakePayloadFile")}
                value={config.udp.fake_payload_file ?? ""}
                options={[
                  {
                    value: "",
                    label: t("sets.udp.fakePayloadFileNone"),
                  },
                  {
                    value: UDP_FAKE_PAYLOAD_AUTO_QUIC,
                    label: t("sets.udp.fakePayloadFileAutoQuicOption"),
                  },
                  {
                    value: UDP_FAKE_PAYLOAD_PRESET_1,
                    label: t("sets.udp.fakePayloadFilePreset1"),
                  },
                  {
                    value: UDP_FAKE_PAYLOAD_PRESET_2,
                    label: t("sets.udp.fakePayloadFilePreset2"),
                  },
                  ...sortedCaptures.map((c) => ({
                    value: c.filepath,
                    label: `[${c.protocol}] ${c.domain} (${c.size} bytes)`,
                  })),
                ]}
                onChange={(e) =>
                  onChange("udp.fake_payload_file", e.target.value)
                }
                helperText={t(payloadFileHelperKey)}
              />
              <B4Hint>
                <Link to="/settings/payloads">
                  {t("sets.udp.fakePayloadManage")}
                </Link>
              </B4Hint>
            </Grid>

            <B4FormHeader label={t("sets.udp.evasionHeader")} />

            <Grid size={{ xs: 12, md: 6 }}>
              <B4Select
                label={t("sets.udp.evasionTechnique")}
                value={config.udp.faking_strategy}
                options={UDP_FAKING_STRATEGIES}
                onChange={(e) =>
                  onChange("udp.faking_strategy", e.target.value)
                }
                helperText={
                  UDP_FAKING_STRATEGIES.find(
                    (o) => o.value === config.udp.faking_strategy,
                  )?.description
                }
              />
            </Grid>

            <Grid size={{ xs: 12, md: 6 }}>
              <B4RangeSlider
                label={t("sets.udp.seg2delay")}
                value={[
                  config.udp.seg2delay,
                  config.udp.seg2delay_max || config.udp.seg2delay,
                ]}
                onChange={(value: [number, number]) => {
                  onChange("udp.seg2delay", value[0]);
                  onChange("udp.seg2delay_max", value[1]);
                }}
                min={0}
                max={1000}
                step={10}
                valueSuffix=" ms"
                helperText={t("sets.udp.seg2delayHelper")}
              />
            </Grid>
          </>
        )}
      </Grid>
    </B4Section>
  );
};
