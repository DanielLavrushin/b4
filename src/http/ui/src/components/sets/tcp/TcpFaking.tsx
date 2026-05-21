import { useCaptures } from "@b4.capture";
import {
  B4Accordion,
  B4Alert,
  B4ChipList,
  B4FormHeader,
  B4Hint,
  B4NumberField,
  B4PlusButton,
  B4Select,
  B4Slider,
  B4Switch,
  B4TextField,
} from "@b4.elements";
import {
  B4SetConfig,
  DesyncMode,
  FakingPayloadType,
  IncomingMode,
  MutationMode,
  WindowMode,
} from "@models/config";
import { Box, Grid, Stack, Typography } from "@mui/material";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { useTranslation } from "react-i18next";

interface TcpFakingProps {
  config: B4SetConfig;
  onChange: (
    field: string,
    value: string | boolean | number | string[] | number[],
  ) => void;
}

export const TcpFaking = ({ config, onChange }: TcpFakingProps) => {
  const { t } = useTranslation();
  const [newFakeSni, setNewFakeSni] = useState("");
  const [newWinValue, setNewWinValue] = useState("");
  const { captures, loadCaptures } = useCaptures();

  useEffect(() => {
    loadCaptures().catch(() => {});
  }, [loadCaptures]);

  const FAKE_STRATEGIES = [
    { value: "ttl", label: t("sets.faking.fakeSni.strategyTtl") },
    { value: "randseq", label: t("sets.faking.fakeSni.strategyRandseq") },
    { value: "pastseq", label: t("sets.faking.fakeSni.strategyPastseq") },
    { value: "tcp_check", label: t("sets.faking.fakeSni.strategyTcpCheck") },
    { value: "md5sum", label: t("sets.faking.fakeSni.strategyMd5sum") },
    { value: "timestamp", label: t("sets.faking.fakeSni.strategyTimestamp") },
  ];

  const FAKE_PAYLOAD_TYPES = [
    { value: 0, label: t("sets.faking.fakeSni.payloadRandom") },
    { value: 2, label: t("sets.faking.fakeSni.payloadGoogle") },
    { value: 3, label: t("sets.faking.fakeSni.payloadDuckDuckGo") },
    { value: 4, label: t("sets.faking.fakeSni.payloadFile") },
    { value: 5, label: t("sets.faking.fakeSni.payloadZeros") },
    { value: 6, label: t("sets.faking.fakeSni.payloadInverted") },
    { value: 7, label: t("sets.faking.fakeSni.payloadDomainOption") },
  ];

  const MUTATION_MODES: { value: MutationMode; label: string }[] = [
    { value: "off", label: t("sets.faking.mutation.modeOff") },
    { value: "random", label: t("sets.faking.mutation.modeRandom") },
    { value: "grease", label: t("sets.faking.mutation.modeGrease") },
    { value: "padding", label: t("sets.faking.mutation.modePadding") },
    { value: "fakeext", label: t("sets.faking.mutation.modeFakeext") },
    { value: "fakesni", label: t("sets.faking.mutation.modeFakesni") },
    { value: "advanced", label: t("sets.faking.mutation.modeAdvanced") },
  ];

  const mutationModeDescriptions: Record<MutationMode, string> = {
    off: t("sets.faking.mutation.descOff"),
    random: t("sets.faking.mutation.descRandom"),
    grease: t("sets.faking.mutation.descGrease"),
    padding: t("sets.faking.mutation.descPadding"),
    fakeext: t("sets.faking.mutation.descFakeext"),
    fakesni: t("sets.faking.mutation.descFakesni"),
    advanced: t("sets.faking.mutation.descAdvanced"),
  };

  const desyncModeOptions: { label: string; value: DesyncMode }[] = [
    { label: t("sets.faking.desync.modeOff"), value: "off" },
    { label: t("sets.faking.desync.modeRst"), value: "rst" },
    { label: t("sets.faking.desync.modeFin"), value: "fin" },
    { label: t("sets.faking.desync.modeAck"), value: "ack" },
    { label: t("sets.faking.desync.modeCombo"), value: "combo" },
    { label: t("sets.faking.desync.modeFull"), value: "full" },
  ];

  const desyncModeDescriptions: Record<DesyncMode, string> = {
    off: t("sets.faking.desync.descOff"),
    rst: t("sets.faking.desync.descRst"),
    fin: t("sets.faking.desync.descFin"),
    ack: t("sets.faking.desync.descAck"),
    combo: t("sets.faking.desync.descCombo"),
    full: t("sets.faking.desync.descFull"),
  };

  const windowModeOptions: { label: string; value: WindowMode }[] = [
    { label: t("sets.faking.window.modeOff"), value: "off" },
    { label: t("sets.faking.window.modeZero"), value: "zero" },
    { label: t("sets.faking.window.modeRandom"), value: "random" },
    { label: t("sets.faking.window.modeOscillate"), value: "oscillate" },
    { label: t("sets.faking.window.modeEscalate"), value: "escalate" },
  ];

  const windowModeDescriptions: Record<WindowMode, string> = {
    off: t("sets.faking.window.descOff"),
    zero: t("sets.faking.window.descZero"),
    random: t("sets.faking.window.descRandom"),
    oscillate: t("sets.faking.window.descOscillate"),
    escalate: t("sets.faking.window.descEscalate"),
  };

  const incomingModeOptions: { label: string; value: IncomingMode }[] = [
    { label: t("sets.faking.incoming.modeOff"), value: "off" },
    { label: t("sets.faking.incoming.modeFake"), value: "fake" },
    { label: t("sets.faking.incoming.modeReset"), value: "reset" },
    { label: t("sets.faking.incoming.modeFin"), value: "fin" },
    { label: t("sets.faking.incoming.modeDesync"), value: "desync" },
  ];

  const incomingModeDescriptions: Record<IncomingMode, string> = {
    off: t("sets.faking.incoming.descOff"),
    fake: t("sets.faking.incoming.descFake"),
    reset: t("sets.faking.incoming.descReset"),
    fin: t("sets.faking.incoming.descFin"),
    desync: t("sets.faking.incoming.descDesync"),
  };

  const incomingStrategyOptions: { label: string; value: string }[] = [
    { label: t("sets.faking.incoming.strategyBadsum"), value: "badsum" },
    { label: t("sets.faking.incoming.strategyBadseq"), value: "badseq" },
    { label: t("sets.faking.incoming.strategyBadack"), value: "badack" },
    { label: t("sets.faking.incoming.strategyRand"), value: "rand" },
    { label: t("sets.faking.incoming.strategyAll"), value: "all" },
  ];

  const incomingStrategyDescriptions: Record<string, string> = {
    badsum: t("sets.faking.incoming.stratDescBadsum"),
    badseq: t("sets.faking.incoming.stratDescBadseq"),
    badack: t("sets.faking.incoming.stratDescBadack"),
    rand: t("sets.faking.incoming.stratDescRand"),
    all: t("sets.faking.incoming.stratDescAll"),
  };

  const mutation = config.faking.sni_mutation || {
    mode: "off",
    grease_count: 3,
    padding_size: 2048,
    fake_ext_count: 5,
    fake_snis: [],
  };

  const isMutationEnabled = mutation.mode !== "off";
  const showGreaseSettings = ["grease", "advanced"].includes(mutation.mode);
  const showPaddingSettings = ["padding", "advanced"].includes(mutation.mode);
  const showFakeExtSettings = ["fakeext", "advanced"].includes(mutation.mode);
  const showFakeSniSettings = ["fakesni", "advanced"].includes(mutation.mode);

  const isDesyncEnabled = config.tcp.desync.mode !== "off";

  const winValues = config.tcp.win.values || [0, 1460, 8192, 65535];
  const showWinValues = ["oscillate", "random"].includes(config.tcp.win.mode);

  // Status summaries for accordion headers
  const fakeSniStatus = config.faking.sni
    ? FAKE_STRATEGIES.find((s) => s.value === config.faking.strategy)?.label ||
      "Enabled"
    : "Disabled";

  const synFakeStatus =
    [config.tcp.syn_fake && "SYN", config.faking.tcp_md5 && "MD5"]
      .filter(Boolean)
      .join(" + ") || "Disabled";

  const desyncStatus = isDesyncEnabled
    ? desyncModeOptions.find((o) => o.value === config.tcp.desync.mode)
        ?.label || "Enabled"
    : "Disabled";

  const windowStatus =
    config.tcp.win.mode === "off"
      ? "Disabled"
      : windowModeOptions.find((o) => o.value === config.tcp.win.mode)?.label ||
        "Enabled";

  const incomingStatus =
    (config.tcp.incoming?.mode || "off") === "off"
      ? "Disabled"
      : incomingModeOptions.find((o) => o.value === config.tcp.incoming?.mode)
          ?.label || "Enabled";

  const mutationStatus = isMutationEnabled
    ? MUTATION_MODES.find((m) => m.value === mutation.mode)?.label || "Enabled"
    : "Disabled";

  const handleAddFakeSni = () => {
    if (newFakeSni.trim()) {
      const current = mutation.fake_snis || [];
      if (!current.includes(newFakeSni.trim())) {
        onChange("faking.sni_mutation.fake_snis", [
          ...current,
          newFakeSni.trim(),
        ]);
      }
      setNewFakeSni("");
    }
  };

  const handleRemoveFakeSni = (sni: string) => {
    const current = mutation.fake_snis || [];
    onChange(
      "faking.sni_mutation.fake_snis",
      current.filter((s) => s !== sni),
    );
  };

  const handleAddWinValue = () => {
    const val = Number.parseInt(newWinValue, 10);
    if (
      !Number.isNaN(val) &&
      val >= 0 &&
      val <= 65535 &&
      !winValues.includes(val)
    ) {
      onChange(
        "tcp.win.values",
        [...winValues, val].sort((a, b) => a - b),
      );
      setNewWinValue("");
    }
  };

  const handleRemoveWinValue = (val: number) => {
    onChange(
      "tcp.win.values",
      winValues.filter((v) => v !== val),
    );
  };

  return (
    <Stack spacing={1.5}>
      {/* Fake SNI Configuration */}
      <B4Accordion
        title={t("sets.faking.fakeSni.title")}
        status={fakeSniStatus}
        enabled={config.faking.sni}
        defaultExpanded
      >
        <Grid container spacing={2}>
          <Grid size={{ xs: 12 }}>
            <B4Switch
              label={t("sets.faking.fakeSni.enable")}
              checked={config.faking.sni}
              onChange={(checked: boolean) => onChange("faking.sni", checked)}
              description={t("sets.faking.fakeSni.enableDesc")}
              aiTopic="faking.sni"
            />
          </Grid>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Select
              label={t("sets.faking.fakeSni.strategy")}
              value={config.faking.strategy}
              options={FAKE_STRATEGIES}
              onChange={(e) => onChange("faking.strategy", e.target.value)}
              helperText={t("sets.faking.fakeSni.strategyHelper")}
              disabled={!config.faking.sni}
              aiTopic="faking.strategy"
              aiContext={{ available: FAKE_STRATEGIES.map((s) => s.value) }}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 6 }}>
            <Stack>
              <B4Select
                label={t("sets.faking.fakeSni.payloadType")}
                value={config.faking.sni_type}
                options={FAKE_PAYLOAD_TYPES}
                onChange={(e) =>
                  onChange("faking.sni_type", Number(e.target.value))
                }
                helperText={t("sets.faking.fakeSni.payloadHelper")}
                disabled={!config.faking.sni}
                aiTopic="faking.sni_type"
                aiContext={{
                  available: FAKE_PAYLOAD_TYPES.map((p) => p.value),
                }}
              />

              {config.faking.sni_type === FakingPayloadType.CUSTOM && (
                <Box sx={{ mt: 2 }}>
                  <B4TextField
                    label={t("sets.faking.fakeSni.customPayload")}
                    value={config.faking.custom_payload}
                    onChange={(e) =>
                      onChange("faking.custom_payload", e.target.value)
                    }
                    helperText={t("sets.faking.fakeSni.customPayloadHelper")}
                    disabled={!config.faking.sni}
                    multiline
                    rows={2}
                  />
                </Box>
              )}
              {config.faking.sni_type === FakingPayloadType.DOMAIN && (
                <Box sx={{ mt: 2 }}>
                  <B4TextField
                    label={t("sets.faking.fakeSni.payloadDomainLabel")}
                    value={config.faking.payload_domain}
                    onChange={(e) =>
                      onChange("faking.payload_domain", e.target.value)
                    }
                    helperText={t("sets.faking.fakeSni.payloadDomainHelper")}
                    disabled={!config.faking.sni}
                    placeholder="example.com"
                  />
                </Box>
              )}
            </Stack>
          </Grid>
          {config.faking.sni_type === FakingPayloadType.CAPTURE && (
            <Grid container size={{ xs: 12 }}>
              {captures.length > 0 && (
                <Grid size={{ xs: 6 }}>
                  <B4Select
                    label={t("sets.faking.fakeSni.generatedPayload")}
                    value={config.faking.payload_file}
                    options={[
                      {
                        value: "",
                        label: t("sets.faking.fakeSni.selectPayload"),
                      },
                      ...captures.map((c) => ({
                        value: c.filepath,
                        label: `${c.domain} (${c.size} bytes)`,
                      })),
                    ]}
                    onChange={(e) =>
                      onChange("faking.payload_file", e.target.value)
                    }
                    helperText={
                      captures.length === 0
                        ? t("sets.faking.fakeSni.noPayloadsHelper")
                        : t("sets.faking.fakeSni.selectPayloadHelper")
                    }
                    disabled={!config.faking.sni || captures.length === 0}
                  />
                </Grid>
              )}
              <Grid size={{ xs: captures.length > 0 ? 6 : 12 }}>
                <B4Alert>
                  {captures.length === 0 && t("sets.faking.fakeSni.noPayloads")}

                  <Link to="/settings/payloads">
                    {" "}
                    {t("sets.faking.fakeSni.navigateSettings")}
                  </Link>
                </B4Alert>
              </Grid>
            </Grid>
          )}
          <Grid size={{ xs: 12, md: 4 }}>
            <B4Slider
              label={t("sets.faking.fakeSni.ttl")}
              value={config.faking.ttl}
              onChange={(value: number) => onChange("faking.ttl", value)}
              min={1}
              max={64}
              step={1}
              helperText={t("sets.faking.fakeSni.ttlHelper")}
              disabled={!config.faking.sni}
            />
          </Grid>
          {(config.faking.strategy === "pastseq" ||
            config.faking.strategy === "randseq") && (
            <Grid size={{ xs: 12, md: 4 }}>
              <B4NumberField
                label={t("sets.faking.fakeSni.seqOffset")}
                value={config.faking.seq_offset}
                onChange={(n) => onChange("faking.seq_offset", n)}
                helperText={t("sets.faking.fakeSni.seqOffsetHelper")}
                disabled={!config.faking.sni}
              />
            </Grid>
          )}
          {config.faking.strategy === "timestamp" && (
            <Grid size={{ xs: 12, md: 4 }}>
              <B4NumberField
                label={t("sets.faking.fakeSni.timestampDecrease")}
                value={config.faking.timestamp_decrease || 600000}
                onChange={(n) => onChange("faking.timestamp_decrease", n)}
                min={0}
                helperText={t("sets.faking.fakeSni.timestampDecreaseHelper")}
                disabled={!config.faking.sni}
              />
            </Grid>
          )}
          <Grid size={{ xs: 12, md: 4 }}>
            <B4Slider
              label={t("sets.faking.fakeSni.packetCount")}
              value={config.faking.sni_seq_length}
              onChange={(value: number) =>
                onChange("faking.sni_seq_length", value)
              }
              min={1}
              max={20}
              step={1}
              helperText={t("sets.faking.fakeSni.packetCountHelper")}
              disabled={!config.faking.sni}
            />
          </Grid>
          {/* TLS Mod Options - only show when payload has TLS structure */}
          {config.faking.sni_type !== FakingPayloadType.RANDOM && (
            <Grid size={{ xs: 12 }}>
              <Typography variant="subtitle2" sx={{ mb: 1 }}>
                {t("sets.faking.fakeSni.tlsModTitle")}
              </Typography>
              <Typography
                variant="caption"
                color="text.secondary"
                sx={{ display: "block", mb: 1 }}
              >
                {t("sets.faking.fakeSni.tlsModDesc")}
              </Typography>
              <Stack direction="row" spacing={2}>
                <B4Switch
                  label={t("sets.faking.fakeSni.randomizeTlsRandom")}
                  checked={(config.faking.tls_mod || []).includes("rnd")}
                  onChange={(checked: boolean) => {
                    const current = config.faking.tls_mod || [];
                    const next = checked
                      ? [...current.filter((m) => m !== "rnd"), "rnd"]
                      : current.filter((m) => m !== "rnd");
                    onChange("faking.tls_mod", next);
                  }}
                  description={t("sets.faking.fakeSni.randomizeTlsRandomDesc")}
                  disabled={!config.faking.sni}
                />
                <B4Switch
                  label={t("sets.faking.fakeSni.dupSessionId")}
                  checked={(config.faking.tls_mod || []).includes("dupsid")}
                  onChange={(checked: boolean) => {
                    const current = config.faking.tls_mod || [];
                    const next = checked
                      ? [...current.filter((m) => m !== "dupsid"), "dupsid"]
                      : current.filter((m) => m !== "dupsid");
                    onChange("faking.tls_mod", next);
                  }}
                  description={t("sets.faking.fakeSni.dupSessionIdDesc")}
                  disabled={!config.faking.sni}
                />
              </Stack>
            </Grid>
          )}
        </Grid>
      </B4Accordion>

      {/* SYN Fake Packets */}
      <B4Accordion
        title={t("sets.faking.synFake.title")}
        status={synFakeStatus}
        enabled={config.tcp.syn_fake || config.faking.tcp_md5}
      >
        <B4Hint>{t("sets.faking.synFake.alert")}</B4Hint>
        <Grid container spacing={3}>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Switch
              label={t("sets.faking.synFake.enable")}
              description={t("sets.faking.synFake.enableDesc")}
              checked={config.tcp.syn_fake || false}
              onChange={(checked) => onChange("tcp.syn_fake", checked)}
              aiTopic="tcp.syn_fake"
            />
          </Grid>

          <Grid size={{ xs: 12, md: 6 }}>
            <B4Switch
              label={t("sets.faking.synFake.md5Enable")}
              description={t("sets.faking.synFake.md5Desc")}
              checked={config.faking.tcp_md5 || false}
              onChange={(checked) => onChange("faking.tcp_md5", checked)}
              aiTopic="faking.tcp_md5"
            />
          </Grid>

          {config.tcp.syn_fake && (
            <>
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Slider
                  label={t("sets.faking.synFake.payloadLen")}
                  value={config.tcp.syn_fake_len || 0}
                  onChange={(value: number) =>
                    onChange("tcp.syn_fake_len", value)
                  }
                  min={0}
                  max={1200}
                  step={64}
                  valueSuffix=" bytes"
                  helperText={t("sets.faking.synFake.payloadLenHelper")}
                />
              </Grid>
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Slider
                  label={t("sets.faking.synFake.ttl")}
                  value={config.tcp.syn_ttl || 0}
                  onChange={(value: number) => onChange("tcp.syn_ttl", value)}
                  min={1}
                  max={100}
                  step={1}
                  valueSuffix=" ms"
                  helperText={t("sets.faking.synFake.ttlHelper")}
                />
              </Grid>
            </>
          )}
        </Grid>
      </B4Accordion>

      {/* TCP Desync Configuration */}
      <B4Accordion
        title={t("sets.faking.desync.title")}
        status={desyncStatus}
        enabled={isDesyncEnabled}
      >
        <B4Hint>{t("sets.faking.desync.alert")}</B4Hint>
        <Grid container spacing={3}>
          <Grid size={{ xs: 12, md: 4 }}>
            <B4Select
              label={t("sets.faking.desync.mode")}
              value={config.tcp.desync.mode}
              options={desyncModeOptions}
              onChange={(e) => onChange("tcp.desync.mode", e.target.value)}
              helperText={desyncModeDescriptions[config.tcp.desync.mode]}
              aiTopic="tcp.desync.mode"
              aiContext={{ available: desyncModeOptions.map((o) => o.value) }}
            />
          </Grid>

          <Grid size={{ xs: 12, md: 4 }}>
            <B4Slider
              label={t("sets.faking.desync.ttl")}
              value={config.tcp.desync.ttl}
              onChange={(value: number) => onChange("tcp.desync.ttl", value)}
              min={1}
              max={50}
              step={1}
              disabled={!isDesyncEnabled}
              helperText={
                isDesyncEnabled
                  ? t("sets.faking.desync.ttlHelperEnabled")
                  : t("sets.faking.desync.ttlHelperDisabled")
              }
            />
          </Grid>

          <Grid size={{ xs: 12, md: 4 }}>
            <B4Slider
              label={t("sets.faking.desync.count")}
              value={config.tcp.desync.count}
              onChange={(value: number) => onChange("tcp.desync.count", value)}
              min={1}
              max={20}
              step={1}
              disabled={!isDesyncEnabled}
              helperText={
                isDesyncEnabled
                  ? t("sets.faking.desync.countHelperEnabled")
                  : t("sets.faking.desync.countHelperDisabled")
              }
            />
          </Grid>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Switch
              label={t("sets.faking.desync.postDesync")}
              description={t("sets.faking.desync.postDesyncDesc")}
              checked={config.tcp.desync.post_desync || false}
              onChange={(checked) =>
                onChange("tcp.desync.post_desync", checked)
              }
              aiTopic="tcp.desync.post_desync"
            />
          </Grid>
        </Grid>
      </B4Accordion>

      {/* TCP Window Manipulation */}
      <B4Accordion
        title={t("sets.faking.window.title")}
        status={windowStatus}
        enabled={config.tcp.win.mode !== "off"}
      >
        <B4Hint>{t("sets.faking.window.alert")}</B4Hint>
        <Grid container spacing={3}>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Select
              label={t("sets.faking.window.mode")}
              value={config.tcp.win.mode}
              options={windowModeOptions}
              onChange={(e) => onChange("tcp.win.mode", e.target.value)}
              helperText={windowModeDescriptions[config.tcp.win.mode]}
              aiTopic="tcp.win.mode"
              aiContext={{ available: windowModeOptions.map((o) => o.value) }}
            />
          </Grid>

          {showWinValues && (
            <Grid size={{ xs: 12 }}>
              <Typography variant="subtitle2" gutterBottom>
                {t("sets.faking.window.customValues")}
              </Typography>
              <Typography variant="caption" color="text.secondary" gutterBottom>
                {config.tcp.win.mode === "oscillate"
                  ? t("sets.faking.window.oscillateHint")
                  : t("sets.faking.window.randomHint")}
              </Typography>

              <Grid container spacing={2} alignItems="center">
                <Grid size={{ xs: 12, md: 6 }}>
                  <Box
                    sx={{
                      display: "flex",
                      gap: 2,
                      mt: 1,
                      alignItems: "center",
                    }}
                  >
                    <B4TextField
                      label={t("sets.faking.window.addValue")}
                      value={newWinValue}
                      onChange={(e) => setNewWinValue(e.target.value)}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") {
                          e.preventDefault();
                          handleAddWinValue();
                        }
                      }}
                      type="number"
                    />

                    <B4PlusButton
                      onClick={handleAddWinValue}
                      disabled={!newWinValue}
                    />
                  </Box>
                </Grid>
                <Grid size={{ xs: 12, md: 6 }}>
                  <B4ChipList
                    items={winValues}
                    getKey={(v) => v}
                    getLabel={(v) => v.toLocaleString()}
                    onDelete={handleRemoveWinValue}
                    emptyMessage={t("sets.faking.window.emptyValues")}
                    showEmpty
                  />
                </Grid>
              </Grid>
            </Grid>
          )}
        </Grid>
      </B4Accordion>

      {/* Incoming Response Bypass */}
      <B4Accordion
        title={t("sets.faking.incoming.title")}
        status={incomingStatus}
        enabled={(config.tcp.incoming?.mode || "off") !== "off"}
      >
        <B4Hint>{t("sets.faking.incoming.alert")}</B4Hint>
        <Grid container spacing={3}>
          <Grid size={{ xs: 12, md: 4 }}>
            <B4Select
              label={t("sets.faking.incoming.mode")}
              value={config.tcp.incoming?.mode || "off"}
              options={incomingModeOptions}
              onChange={(e) => onChange("tcp.incoming.mode", e.target.value)}
              helperText={
                incomingModeDescriptions[config.tcp.incoming?.mode || "off"]
              }
              aiTopic="tcp.incoming.mode"
              aiContext={{ available: incomingModeOptions.map((o) => o.value) }}
            />
          </Grid>

          <Grid size={{ xs: 12, md: 4 }}>
            <B4Select
              label={t("sets.faking.incoming.strategy")}
              value={config.tcp.incoming?.strategy || "badsum"}
              options={incomingStrategyOptions}
              onChange={(e) =>
                onChange("tcp.incoming.strategy", e.target.value)
              }
              disabled={config.tcp.incoming?.mode === "off"}
              helperText={
                config.tcp.incoming?.mode === "off"
                  ? t("sets.faking.incoming.enableFirst")
                  : incomingStrategyDescriptions[
                      config.tcp.incoming?.strategy || "badsum"
                    ]
              }
              aiTopic="tcp.incoming.strategy"
              aiContext={{
                available: incomingStrategyOptions.map((o) => o.value),
              }}
            />
          </Grid>

          <Grid size={{ xs: 12, md: 4 }}>
            <B4Slider
              label={t("sets.faking.incoming.ttl")}
              value={config.tcp.incoming?.fake_ttl || 3}
              onChange={(value: number) =>
                onChange("tcp.incoming.fake_ttl", value)
              }
              min={1}
              max={20}
              step={1}
              disabled={config.tcp.incoming?.mode === "off"}
              helperText={t("sets.faking.incoming.ttlHelper")}
            />
          </Grid>

          <Grid size={{ xs: 12, md: 4 }}>
            <B4Slider
              label={t("sets.faking.incoming.fakeCount")}
              value={config.tcp.incoming?.fake_count || 3}
              onChange={(value: number) =>
                onChange("tcp.incoming.fake_count", value)
              }
              min={1}
              max={10}
              step={1}
              disabled={config.tcp.incoming?.mode === "off"}
              helperText={t("sets.faking.incoming.fakeCountHelper")}
            />
          </Grid>

          <Grid size={{ xs: 12, md: 4 }}>
            <B4Slider
              label={t("sets.faking.incoming.thresholdMin")}
              value={config.tcp.incoming?.min || 14}
              onChange={(value: number) => onChange("tcp.incoming.min", value)}
              min={5}
              max={config.tcp.incoming?.max || 150}
              step={1}
              valueSuffix=" KB"
              disabled={
                config.tcp.incoming?.mode === "off" ||
                config.tcp.incoming?.mode === "fake"
              }
              helperText={
                config.tcp.incoming?.mode === "fake"
                  ? t("sets.faking.incoming.thresholdMinFake")
                  : t("sets.faking.incoming.thresholdMinHelper")
              }
            />
          </Grid>

          <Grid size={{ xs: 12, md: 4 }}>
            <B4Slider
              label={t("sets.faking.incoming.thresholdMax")}
              value={config.tcp.incoming?.max || 14}
              onChange={(value: number) => onChange("tcp.incoming.max", value)}
              min={config.tcp.incoming?.min || 5}
              max={50}
              step={1}
              valueSuffix=" KB"
              disabled={
                config.tcp.incoming?.mode === "off" ||
                config.tcp.incoming?.mode === "fake"
              }
              helperText={
                config.tcp.incoming?.mode === "fake"
                  ? t("sets.faking.incoming.thresholdMaxFake")
                  : config.tcp.incoming?.min === config.tcp.incoming?.max
                    ? t("sets.faking.incoming.thresholdMaxFixed")
                    : t("sets.faking.incoming.thresholdMaxHelper")
              }
            />
          </Grid>
        </Grid>
      </B4Accordion>

      {/* ClientHello Mutation Section */}
      <B4Accordion
        title={t("sets.faking.mutation.title")}
        status={mutationStatus}
        enabled={isMutationEnabled}
      >
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Select
              label={t("sets.faking.mutation.mode")}
              value={mutation.mode}
              options={MUTATION_MODES}
              onChange={(e) =>
                onChange("faking.sni_mutation.mode", e.target.value)
              }
              helperText={mutationModeDescriptions[mutation.mode]}
              aiTopic="faking.sni_mutation.mode"
              aiContext={{ available: MUTATION_MODES.map((m) => m.value) }}
            />
          </Grid>

          {isMutationEnabled && (
            <>
              {showGreaseSettings && (
                <>
                  <B4FormHeader
                    label={t("sets.faking.mutation.greaseHeader")}
                  />
                  <Grid size={{ xs: 12 }}>
                    <B4Slider
                      label={t("sets.faking.mutation.greaseCount")}
                      value={mutation.grease_count}
                      onChange={(value: number) =>
                        onChange("faking.sni_mutation.grease_count", value)
                      }
                      min={1}
                      max={10}
                      step={1}
                      helperText={t("sets.faking.mutation.greaseCountHelper")}
                    />
                  </Grid>
                </>
              )}

              {showPaddingSettings && (
                <>
                  <B4FormHeader
                    label={t("sets.faking.mutation.paddingHeader")}
                  />
                  <Grid size={{ xs: 12 }}>
                    <B4Slider
                      label={t("sets.faking.mutation.paddingSize")}
                      value={mutation.padding_size}
                      onChange={(value: number) =>
                        onChange("faking.sni_mutation.padding_size", value)
                      }
                      min={256}
                      max={16384}
                      step={256}
                      valueSuffix=" bytes"
                      helperText={t("sets.faking.mutation.paddingSizeHelper")}
                    />
                  </Grid>
                </>
              )}

              {showFakeExtSettings && (
                <>
                  <B4FormHeader
                    label={t("sets.faking.mutation.fakeExtHeader")}
                  />
                  <Grid size={{ xs: 12 }}>
                    <B4Slider
                      label={t("sets.faking.mutation.fakeExtCount")}
                      value={mutation.fake_ext_count}
                      onChange={(value: number) =>
                        onChange("faking.sni_mutation.fake_ext_count", value)
                      }
                      min={1}
                      max={15}
                      step={1}
                      helperText={t("sets.faking.mutation.fakeExtCountHelper")}
                    />
                  </Grid>
                </>
              )}

              {showFakeSniSettings && (
                <>
                  <B4FormHeader
                    label={t("sets.faking.mutation.fakeSniHeader")}
                  />
                  <Grid size={{ xs: 12, md: 6 }}>
                    <Box
                      sx={{ display: "flex", gap: 1, alignItems: "flex-start" }}
                    >
                      <B4TextField
                        label={t("sets.faking.mutation.addFakeSni")}
                        value={newFakeSni}
                        onChange={(e) => setNewFakeSni(e.target.value)}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") {
                            e.preventDefault();
                            handleAddFakeSni();
                          }
                        }}
                        placeholder={t(
                          "sets.faking.mutation.fakeSniPlaceholder",
                        )}
                        helperText={t("sets.faking.mutation.fakeSniHelper")}
                      />
                      <B4PlusButton
                        onClick={handleAddFakeSni}
                        disabled={!newFakeSni.trim()}
                      />
                    </Box>
                  </Grid>
                  <B4ChipList
                    items={mutation.fake_snis || []}
                    getKey={(s) => s}
                    getLabel={(s) => s}
                    onDelete={handleRemoveFakeSni}
                    title={t("sets.faking.mutation.activeFakeSnisTitle")}
                    gridSize={{ xs: 12, md: 6 }}
                  />
                </>
              )}
            </>
          )}
        </Grid>
      </B4Accordion>
    </Stack>
  );
};
