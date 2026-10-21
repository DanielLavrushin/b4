import { useEffect } from "react";
import { Autocomplete, Grid, Stack, Typography } from "@mui/material";
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
  B4Badge,
} from "@b4.elements";
import { colors } from "@design";
import {
  B4SetConfig,
  QueueConfig,
  UdpMode,
  UDP_FAKE_PAYLOAD_AUTO_QUIC,
  UDP_FAKE_PAYLOAD_AUTO_STUN,
  UDP_FAKE_PAYLOAD_PRESET_1,
  UDP_FAKE_PAYLOAD_PRESET_2,
} from "@models/config";
import { useCaptures } from "@b4.capture";
import { useTranslation, Trans } from "react-i18next";
import { UDP_L7_CLASSES, udpL7Class, udpL7Label } from "./udpL7";

interface UdpSettingsProps {
  config: B4SetConfig;
  queue: QueueConfig;
  onChange: (
    field: string,
    value: string | boolean | number | string[],
  ) => void;
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
    {
      value: "coalesce",
      label: t("sets.udp.modeCoalesce"),
      description: t("sets.udp.modeCoalesceDesc"),
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

  const hasIPTargets =
    (config.targets?.ip?.length ?? 0) > 0 ||
    (config.targets?.geoip_categories?.length ?? 0) > 0 ||
    (config.targets?.asns?.length ?? 0) > 0;

  const hasNonDomainTargets =
    hasIPTargets || (config.udp.dport_filter ?? "").trim() !== "";

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

  const filterL7 = config.udp.filter_l7 ?? [];
  const selectsL7 = filterL7.length > 0;
  const hasDestinationTargets = hasDomainsConfigured || hasIPTargets;
  const hasTcpPortFilter = (config.tcp.dport_filter ?? "").trim() !== "";
  const hasRoutingSourceIfaces =
    config.routing.enabled &&
    (config.routing.source_interfaces ?? []).some(
      (iface) => iface.trim() !== "",
    );
  const changeL7 = (next: string[]) => {
    onChange("udp.filter_l7", next);
    const zeroFill = (config.udp.fake_payload_file ?? "") === "";
    if (next.length > 0 && !selectsL7 && zeroFill) {
      onChange("udp.fake_payload_file", UDP_FAKE_PAYLOAD_AUTO_STUN);
    }
  };
  const l7Label = (value: string) => udpL7Label(t, value);

  const showFakeSettings = config.udp.mode === "fake";
  const isAutoQuic =
    config.udp.fake_payload_file === UDP_FAKE_PAYLOAD_AUTO_QUIC;
  const isAutoStun =
    config.udp.fake_payload_file === UDP_FAKE_PAYLOAD_AUTO_STUN;

  let payloadFileHelperKey: string;
  if (isAutoQuic) {
    payloadFileHelperKey = "sets.udp.fakePayloadFileAutoQuic";
  } else if (isAutoStun) {
    payloadFileHelperKey = "sets.udp.fakePayloadFileAutoStun";
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
            description={t(
              selectsL7
                ? "sets.udp.filterStunL7Desc"
                : "sets.udp.filterStunDesc",
            )}
            disabled={selectsL7}
          />
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <Autocomplete
            multiple
            disableCloseOnSelect
            size="small"
            options={UDP_L7_CLASSES.map((entry) => entry.value)}
            value={filterL7}
            onChange={(_, next) => changeL7(next)}
            getOptionLabel={l7Label}
            renderOption={(props, value) => {
              const { key, ...optionProps } = props;
              const entry = udpL7Class(value);
              return (
                <li key={key} {...optionProps}>
                  <Stack>
                    <Typography variant="body2">{l7Label(value)}</Typography>
                    {entry && (
                      <Typography
                        variant="caption"
                        sx={{ color: colors.text.secondary }}
                      >
                        {t(entry.descriptionKey)}
                      </Typography>
                    )}
                  </Stack>
                </li>
              );
            }}
            renderInput={(params) => (
              <B4TextField
                {...params}
                label={t("sets.udp.filterL7")}
                helperText={t("sets.udp.filterL7Helper")}
              />
            )}
            renderValue={(selected, getItemProps) =>
              selected.map((value, index) => {
                const { key, ...itemProps } = getItemProps({ index });
                return (
                  <B4Badge
                    key={key}
                    {...itemProps}
                    label={l7Label(value)}
                    size="small"
                    sx={{
                      bgcolor: colors.accent.secondary,
                      border: `1px solid ${colors.secondary}`,
                    }}
                  />
                );
              })
            }
          />
        </Grid>

        {selectsL7 && hasDestinationTargets && (
          <B4Alert severity="warning" icon={<WarningIcon />}>
            <Trans i18nKey="sets.udp.filterL7TargetsWarning" />
          </B4Alert>
        )}
        {selectsL7 && (config.udp.dport_filter ?? "").trim() === "" && (
          <B4Alert severity="warning" icon={<WarningIcon />}>
            <Trans i18nKey="sets.udp.filterL7PortsWarning" />
          </B4Alert>
        )}
        {selectsL7 && hasTcpPortFilter && (
          <B4Alert severity="warning" icon={<WarningIcon />}>
            <Trans i18nKey="sets.udp.filterL7TcpPortsWarning" />
          </B4Alert>
        )}
        {selectsL7 && hasRoutingSourceIfaces && (
          <B4Alert severity="warning" icon={<WarningIcon />}>
            <Trans i18nKey="sets.udp.filterL7SourceInterfacesWarning" />
          </B4Alert>
        )}

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
                coalesce: "sets.udp.coalesceModeInfo",
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
                    value: UDP_FAKE_PAYLOAD_AUTO_STUN,
                    label: t("sets.udp.fakePayloadFileAutoStunOption"),
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
