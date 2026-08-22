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
      value: "off",
      label: t("sets.udp.modeOff"),
      description: t("sets.udp.modeOffDesc"),
    },
    {
      value: "fake",
      label: t("sets.udp.modeFake"),
      description: t("sets.udp.modeFakeDesc"),
    },
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
  ];

  const UDP_QUIC_FILTERS = [
    {
      value: "sni",
      label: t("sets.udp.quicSni"),
      description: t("sets.udp.quicSniDesc"),
    },
    {
      value: "all",
      label: t("sets.udp.quicAll"),
      description: t("sets.udp.quicAllDesc"),
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

  const hasDomainsConfigured =
    config.targets?.sni_domains?.length > 0 ||
    config.targets?.geosite_categories?.length > 0;

  const hasNonDomainTargets =
    (config.targets?.ip?.length ?? 0) > 0 ||
    (config.targets?.geoip_categories?.length ?? 0) > 0 ||
    (config.udp.dport_filter ?? "").trim() !== "";

  const isPassthrough = config.udp.mode === "off";
  const blockQuic =
    config.udp.filter_quic === "all" && config.udp.mode === "reject";

  const toggleBlockQuic = (checked: boolean) => {
    if (checked) {
      onChange("udp.filter_quic", "all");
      onChange("udp.mode", "reject");
      return;
    }
    onChange("udp.filter_quic", "sni");
    onChange("udp.mode", "fake");
  };

  const showFakeSettings = config.udp.mode === "fake";
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

  const showSniWarning =
    !isPassthrough &&
    config.udp.filter_quic === "sni" &&
    !hasDomainsConfigured &&
    !hasNonDomainTargets;

  return (
    <B4Section
      title={t("sets.udp.sectionTitle")}
      description={t("sets.udp.sectionDescription")}
      icon={<DnsIcon />}
    >
      <Grid container spacing={3}>
        <B4FormHeader label={t("sets.udp.blockHeader")} />

        <Grid size={{ xs: 12, md: 6 }}>
          <B4Switch
            label={t("sets.udp.blockQuic")}
            checked={blockQuic}
            onChange={toggleBlockQuic}
            description={t("sets.udp.blockQuicDesc")}
          />
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <B4Hint>
            <Trans i18nKey="sets.udp.blockQuicInfo" />
          </B4Hint>
        </Grid>

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
        <Grid size={{ xs: 12, md: 6 }}>
          <B4Switch
            label={t("sets.udp.filterStun")}
            checked={config.udp.filter_stun}
            onChange={(checked) => onChange("udp.filter_stun", checked)}
            description={t("sets.udp.filterStunDesc")}
          />
        </Grid>

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

        {showSniWarning && (
          <B4Alert severity="warning" icon={<WarningIcon />}>
            <Trans i18nKey="sets.udp.sniWarning" />
          </B4Alert>
        )}

        <B4FormHeader label={t("sets.udp.actionHeader")} />

        <Grid size={{ xs: 12, md: 6 }}>
          <B4Select
            label={t("sets.udp.actionMode")}
            value={config.udp.mode}
            options={UDP_MODES}
            onChange={(e) => onChange("udp.mode", e.target.value)}
            helperText={
              UDP_MODES.find((o) => o.value === config.udp.mode)?.description
            }
          />
        </Grid>
        <Grid size={{ xs: 12, md: 6 }}>
          <B4Hint>
            {(() => {
              const infoKeys: Record<UdpMode, string> = {
                off: "sets.udp.offModeInfo",
                fake: "sets.udp.fakeModeInfo",
                reject: "sets.udp.rejectModeInfo",
                drop: "sets.udp.dropModeInfo",
              };
              return (
                <Trans i18nKey={infoKeys[config.udp.mode] || infoKeys.fake} />
              );
            })()}
          </B4Hint>
        </Grid>

        {isPassthrough && (
          <B4Alert>
            <Trans i18nKey="sets.udp.noProcessingWarning" />
          </B4Alert>
        )}

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
