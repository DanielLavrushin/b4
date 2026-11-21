import {
  B4Accordion,
  B4ChipList,
  B4FieldMessage,
  B4FormHeader,
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
  IncomingStrategy,
  MutationMode,
  WindowMode,
} from "@models/config";
import { Box, Grid, Link, Stack, Typography } from "@mui/material";
import { useState } from "react";
import { Link as RouterLink } from "react-router";
import { Trans, useTranslation } from "react-i18next";
import { useChangedFields } from "@context/ChangedFieldsContext";
import {
  byPayloadName,
  chosenPayloadFile,
  isEmptyPayload,
  payloadNote,
  samePayloadFile,
  useListedCaptures,
  useResolvedPayloadFile,
} from "../payloadFile";
import { sendsFake, tlsModChangesTheFake } from "./fakePayload";

interface TcpFakingProps {
  config: B4SetConfig;
  onChange: (
    field: string,
    value: string | boolean | number | string[] | number[],
  ) => void;
}
const ONE_PACKET_TCP_PAYLOAD = 1448;

export const FAKING_ACCORDIONS: { labelKey: string; prefixes: string[] }[] = [
  { labelKey: "sets.faking.fakeSni.title", prefixes: ["faking.sni", "faking.sni_seq_length", "faking.sni_type", "faking.custom_payload", "faking.payload_domain", "faking.fake_len_mode", "faking.payload_file", "faking.strategy", "faking.seq_offset", "faking.timestamp_decrease", "faking.tls_mod", "faking.ttl", "faking.apply_ttl", "faking.md5_on_fake"] },
  { labelKey: "sets.faking.synFake.title", prefixes: ["tcp.syn_fake", "tcp.syn_fake_len", "tcp.syn_ttl", "faking.tcp_md5"] },
  { labelKey: "sets.faking.desync.title", prefixes: ["tcp.desync"] },
  { labelKey: "sets.faking.window.title", prefixes: ["tcp.win"] },
  { labelKey: "sets.faking.incoming.title", prefixes: ["tcp.incoming"] },
  { labelKey: "sets.faking.mutation.title", prefixes: ["faking.sni_mutation"] },
];
export const TcpFaking = ({ config, onChange }: TcpFakingProps) => {
  const { t } = useTranslation();
  const { changedPaths } = useChangedFields();
  const hasSubChanges = (prefixes: string[]): boolean =>
    prefixes.some((prefix) => {
      for (const path of changedPaths) {
        if (path === prefix || path.startsWith(`${prefix}.`)) return true;
      }
      return false;
    });
  const [newFakeSni, setNewFakeSni] = useState("");
  const [newWinValue, setNewWinValue] = useState("");
  const {
    captures,
    listed: capturesListed,
    listError,
  } = useListedCaptures();
  const tlsCaptures = captures
    .filter((c) => c.protocol === "tls" && !c.missing && !isEmptyPayload(c))
    .sort(byPayloadName);
  const payloadFile = config.faking.payload_file;
  const chosenPayloadPath =
    config.faking.sni_type === FakingPayloadType.CAPTURE ? payloadFile : "";
  const resolvedFile = useResolvedPayloadFile(
    captures,
    capturesListed,
    chosenPayloadPath,
  );
  const chosenFile = chosenPayloadFile(
    captures,
    capturesListed,
    chosenPayloadPath,
    resolvedFile,
  );
  const FAKE_STRATEGIES = [
    {
      value: "ttl",
      label: t("sets.faking.fakeSni.strategyTtl"),
      note: t("sets.faking.fakeSni.strategyTtlNote"),
    },
    {
      value: "randseq",
      label: t("sets.faking.fakeSni.strategyRandseq"),
      note: t("sets.faking.fakeSni.strategyRandseqNote"),
    },
    {
      value: "pastseq",
      label: t("sets.faking.fakeSni.strategyPastseq"),
      note: t("sets.faking.fakeSni.strategyPastseqNote"),
    },
    {
      value: "tcp_check",
      label: t("sets.faking.fakeSni.strategyTcpCheck"),
      note: t("sets.faking.fakeSni.strategyTcpCheckNote"),
    },
    {
      value: "md5sum",
      label: t("sets.faking.fakeSni.strategyMd5sum"),
      note: t("sets.faking.fakeSni.strategyMd5sumNote"),
    },
    {
      value: "timestamp",
      label: t("sets.faking.fakeSni.strategyTimestamp"),
      note: t("sets.faking.fakeSni.strategyTimestampNote"),
    },
  ];

  const FAKE_PAYLOAD_TYPES = [
    {
      value: 0,
      label: t("sets.faking.fakeSni.payloadRandom"),
      note: t("sets.faking.fakeSni.payloadRandomNote"),
    },
    {
      value: 2,
      label: t("sets.faking.fakeSni.payloadGoogle"),
      note: t("sets.faking.fakeSni.payloadPresetNote"),
    },
    {
      value: 3,
      label: t("sets.faking.fakeSni.payloadVk"),
      note: t("sets.faking.fakeSni.payloadPresetNote"),
    },
    {
      value: 8,
      label: t("sets.faking.fakeSni.payloadStun"),
      note: t("sets.faking.fakeSni.payloadStunNote"),
    },
    {
      value: 4,
      label: t("sets.faking.fakeSni.payloadFile"),
      note: t("sets.faking.fakeSni.payloadFileNote"),
    },
    {
      value: 5,
      label: t("sets.faking.fakeSni.payloadZeros"),
      note: t("sets.faking.fakeSni.payloadZerosNote"),
    },
    {
      value: 6,
      label: t("sets.faking.fakeSni.payloadInverted"),
      note: t("sets.faking.fakeSni.payloadInvertedNote"),
    },
    {
      value: 7,
      label: t("sets.faking.fakeSni.payloadDomainOption"),
      note: t("sets.faking.fakeSni.payloadDomainNote"),
    },
    ...(config.faking.sni_type === FakingPayloadType.CUSTOM ||
    config.faking.custom_payload !== ""
      ? [
          {
            value: 1,
            label: t("sets.faking.fakeSni.payloadCustom"),
            note: t("sets.faking.fakeSni.payloadCustomNote"),
          },
        ]
      : []),
  ];

  const MUTATION_MODES: { value: MutationMode; label: string; note: string }[] =
    [
      {
        value: "off",
        label: t("sets.faking.mutation.modeOff"),
        note: t("sets.faking.mutation.descOff"),
      },
      {
        value: "random",
        label: t("sets.faking.mutation.modeRandom"),
        note: t("sets.faking.mutation.descRandom"),
      },
      {
        value: "grease",
        label: t("sets.faking.mutation.modeGrease"),
        note: t("sets.faking.mutation.descGrease"),
      },
      {
        value: "padding",
        label: t("sets.faking.mutation.modePadding"),
        note: t("sets.faking.mutation.descPadding"),
      },
      {
        value: "fakeext",
        label: t("sets.faking.mutation.modeFakeext"),
        note: t("sets.faking.mutation.descFakeext"),
      },
      {
        value: "fakesni",
        label: t("sets.faking.mutation.modeFakesni"),
        note: t("sets.faking.mutation.descFakesni"),
      },
      {
        value: "advanced",
        label: t("sets.faking.mutation.modeAdvanced"),
        note: t("sets.faking.mutation.descAdvanced"),
      },
    ];

  const desyncModeOptions: { label: string; value: DesyncMode; note: string }[] =
    [
      {
        label: t("sets.faking.desync.modeOff"),
        value: "off",
        note: t("sets.faking.desync.descOff"),
      },
      {
        label: t("sets.faking.desync.modeRst"),
        value: "rst",
        note: t("sets.faking.desync.descRst"),
      },
      {
        label: t("sets.faking.desync.modeFin"),
        value: "fin",
        note: t("sets.faking.desync.descFin"),
      },
      {
        label: t("sets.faking.desync.modeAck"),
        value: "ack",
        note: t("sets.faking.desync.descAck"),
      },
      {
        label: t("sets.faking.desync.modeCombo"),
        value: "combo",
        note: t("sets.faking.desync.descCombo"),
      },
      {
        label: t("sets.faking.desync.modeFull"),
        value: "full",
        note: t("sets.faking.desync.descFull"),
      },
    ];

  const windowModeOptions: { label: string; value: WindowMode; note: string }[] =
    [
      {
        label: t("sets.faking.window.modeOff"),
        value: "off",
        note: t("sets.faking.window.descOff"),
      },
      {
        label: t("sets.faking.window.modeZero"),
        value: "zero",
        note: t("sets.faking.window.descZero"),
      },
      {
        label: t("sets.faking.window.modeRandom"),
        value: "random",
        note: t("sets.faking.window.descRandom"),
      },
      {
        label: t("sets.faking.window.modeOscillate"),
        value: "oscillate",
        note: t("sets.faking.window.descOscillate"),
      },
      {
        label: t("sets.faking.window.modeEscalate"),
        value: "escalate",
        note: t("sets.faking.window.descEscalate"),
      },
    ];

  const incomingModeOptions: {
    label: string;
    value: IncomingMode;
    note: string;
  }[] = [
    {
      label: t("sets.faking.incoming.modeOff"),
      value: "off",
      note: t("sets.faking.incoming.descOff"),
    },
    {
      label: t("sets.faking.incoming.modeFake"),
      value: "fake",
      note: t("sets.faking.incoming.descFake"),
    },
    {
      label: t("sets.faking.incoming.modeReset"),
      value: "reset",
      note: t("sets.faking.incoming.descReset"),
    },
    {
      label: t("sets.faking.incoming.modeFin"),
      value: "fin",
      note: t("sets.faking.incoming.descFin"),
    },
    {
      label: t("sets.faking.incoming.modeDesync"),
      value: "desync",
      note: t("sets.faking.incoming.descDesync"),
    },
  ];

  const incomingStrategyOptions: {
    label: string;
    value: IncomingStrategy;
    note: string;
  }[] = [
    {
      label: t("sets.faking.incoming.strategyBadsum"),
      value: "badsum",
      note: t("sets.faking.incoming.stratDescBadsum"),
    },
    {
      label: t("sets.faking.incoming.strategyBadseq"),
      value: "badseq",
      note: t("sets.faking.incoming.stratDescBadseq"),
    },
    {
      label: t("sets.faking.incoming.strategyBadack"),
      value: "badack",
      note: t("sets.faking.incoming.stratDescBadack"),
    },
    {
      label: t("sets.faking.incoming.strategyRand"),
      value: "rand",
      note: t("sets.faking.incoming.stratDescRand"),
    },
    {
      label: t("sets.faking.incoming.strategyAll"),
      value: "all",
      note: t("sets.faking.incoming.stratDescAll"),
    },
  ];

  const mutation = config.faking.sni_mutation || {
    mode: "off",
    grease_count: 3,
    padding_size: 2048,
    fake_ext_count: 5,
    fake_snis: [],
  };

  const selectedCapture =
    chosenFile.state === "listed" ? chosenFile.capture : undefined;
  const fakeSent = sendsFake(config);
  const payloadFileNotChosen =
    fakeSent &&
    config.faking.sni_type === FakingPayloadType.CAPTURE &&
    !payloadFile;
  const payloadDomainNotGiven =
    fakeSent &&
    config.faking.sni_type === FakingPayloadType.DOMAIN &&
    !(config.faking.payload_domain || "").trim();

  let chosenFileOption: { label: string; note?: string } | undefined;
  let payloadFileIssue: string | undefined;
  if (chosenFile.state === "missing") {
    chosenFileOption = {
      label: t("sets.faking.fakeSni.payloadFileMissing", {
        name: chosenFile.name,
      }),
      note: t("sets.faking.fakeSni.payloadFileMissingNote"),
    };
    payloadFileIssue = t("sets.faking.fakeSni.payloadFileMissingHelper", {
      file: payloadFile,
    });
  } else if (chosenFile.state === "empty") {
    chosenFileOption = {
      label: t("sets.faking.fakeSni.payloadFileEmpty", {
        name: chosenFile.name,
      }),
      note: t("sets.faking.fakeSni.payloadFileMissingNote"),
    };
    payloadFileIssue = t("sets.faking.fakeSni.payloadFileEmptyHelper", {
      file: payloadFile,
    });
  } else if (chosenFile.state === "unlisted" && chosenFile.tooLarge) {
    chosenFileOption = {
      label: t("sets.faking.fakeSni.payloadFileTooLarge", {
        name: chosenFile.name,
      }),
      note: t("sets.faking.fakeSni.payloadFileMissingNote"),
    };
    payloadFileIssue = t("sets.faking.fakeSni.payloadFileTooLargeHelper", {
      file: payloadFile,
    });
  } else if (chosenFile.state === "unlisted") {
    chosenFileOption = {
      label: t("sets.faking.fakeSni.payloadFileUnlisted", {
        name: chosenFile.name,
      }),
      note: payloadNote(t, chosenFile),
    };
    payloadFileIssue = t("sets.faking.fakeSni.payloadFileUnlistedHelper", {
      file: payloadFile,
    });
  } else if (chosenFile.state === "unknown") {
    chosenFileOption = { label: chosenFile.name };
  } else if (
    chosenFile.state === "listed" &&
    chosenFile.capture.protocol !== "tls"
  ) {
    chosenFileOption = {
      label: t("sets.faking.fakeSni.payloadFileQuic", {
        name: chosenFile.capture.domain,
      }),
      note: payloadNote(t, chosenFile.capture),
    };
    payloadFileIssue = t("sets.faking.fakeSni.payloadFileQuicHelper");
  }
  const payloadFileSentAsIs =
    chosenFile.state === "unlisted" && !chosenFile.tooLarge;

  const payloadFileOptions = [
    { value: "", label: t("sets.faking.fakeSni.selectPayload") },
    ...(chosenFileOption ? [{ value: payloadFile, ...chosenFileOption }] : []),
    ...tlsCaptures.map((c) => ({
      value:
        c === selectedCapture || samePayloadFile(c.filepath, payloadFile)
          ? payloadFile
          : c.filepath,
      label: c.domain,
      note: payloadNote(t, c),
    })),
  ];

  const chosenSize =
    chosenFile.state === "unlisted" && !chosenFile.tooLarge
      ? chosenFile.size
      : selectedCapture?.size;
  const oversizedSize =
    config.faking.sni &&
    config.faking.fake_len_mode !== "match" &&
    chosenSize !== undefined &&
    chosenSize > ONE_PACKET_TCP_PAYLOAD
      ? chosenSize
      : undefined;

  const payloadsLink = <Link component={RouterLink} to="/settings/payloads" />;
  const payloadFileInfo = (
    <Stack spacing={1.5}>
      <span>{t("sets.faking.fakeSni.generatedPayloadInfo")}</span>
      <span>
        {!listError && tlsCaptures.length === 0 && (
          <>{t("sets.faking.fakeSni.noPayloads")} </>
        )}
        <Trans
          i18nKey="sets.faking.fakeSni.navigateSettings"
          components={{ a: payloadsLink }}
        />
      </span>
    </Stack>
  );
  const payloadFileMessages = [
    listError && (
      <B4FieldMessage key="list" severity="error">
        {t("settings.Capture.listFailed", { error: listError })}
      </B4FieldMessage>
    ),
    payloadFileIssue && (
      <B4FieldMessage
        key="file"
        severity={payloadFileSentAsIs ? "info" : "warning"}
      >
        {payloadFileIssue}
      </B4FieldMessage>
    ),
    payloadFileNotChosen && (
      <B4FieldMessage key="unchosen">
        {t("sets.faking.fakeSni.payloadFileNotChosen")}
      </B4FieldMessage>
    ),
    !listError && capturesListed && tlsCaptures.length === 0 && (
      <B4FieldMessage key="none" severity="info">
        <Trans
          i18nKey="sets.faking.fakeSni.noPayloadsHelper"
          components={{ a: payloadsLink }}
        />
      </B4FieldMessage>
    ),
    oversizedSize !== undefined && (
      <B4FieldMessage key="oversized">
        {t("sets.faking.fakeSni.payloadOverOnePacket", {
          count: oversizedSize,
          max: ONE_PACKET_TCP_PAYLOAD,
        })}
      </B4FieldMessage>
    ),
  ].filter(Boolean);

  const isMutationEnabled = mutation.mode !== "off";
  const showGreaseSettings = ["grease", "advanced"].includes(mutation.mode);
  const showPaddingSettings = ["padding", "advanced"].includes(mutation.mode);
  const showFakeExtSettings = ["fakeext", "advanced"].includes(mutation.mode);
  const showFakeSniSettings = ["fakesni", "advanced"].includes(mutation.mode);

  const isDesyncEnabled = config.tcp.desync.mode !== "off";
  const desyncCountUsed = ["rst", "fin", "ack"].includes(
    config.tcp.desync.mode,
  );

  const winValues = config.tcp.win.values || [0, 1460, 8192, 65535];
  const showWinValues = ["oscillate", "random"].includes(config.tcp.win.mode);

  // Status summaries for accordion headers
  const enabledStatus = t("core.enabled");
  const disabledStatus = t("core.disabled");
  const fakeSniStatus = config.faking.sni
    ? FAKE_STRATEGIES.find((s) => s.value === config.faking.strategy)?.label ||
      enabledStatus
    : disabledStatus;

  const synFakeStatus =
    [config.tcp.syn_fake && "SYN", config.faking.tcp_md5 && "MD5"]
      .filter(Boolean)
      .join(" + ") || disabledStatus;

  const isFakeTtlActive =
    (config.faking.sni &&
      (config.faking.strategy === "ttl" || !!config.faking.apply_ttl)) ||
    !!config.faking.tcp_md5 ||
    (!!config.tcp.syn_fake && !config.tcp.syn_ttl) ||
    config.fragmentation.strategy === "oob" ||
    !!config.fragmentation.strategy_pool?.includes("oob") ||
    !!config.fragmentation.combo?.decoy_enabled ||
    (config.fragmentation.seq_overlap_pattern?.length ?? 0) > 0 ||
    !!config.tcp.desync.post_desync ||
    config.udp.mode === "fake";

  const incomingMode = config.tcp.incoming?.mode || "off";
  const incomingStrategy = config.tcp.incoming?.strategy || "badsum";

  const desyncStatus = isDesyncEnabled
    ? desyncModeOptions.find((o) => o.value === config.tcp.desync.mode)
        ?.label || enabledStatus
    : disabledStatus;

  const windowStatus =
    config.tcp.win.mode === "off"
      ? disabledStatus
      : windowModeOptions.find((o) => o.value === config.tcp.win.mode)?.label ||
        enabledStatus;

  const incomingStatus =
    incomingMode === "off"
      ? disabledStatus
      : incomingModeOptions.find((o) => o.value === incomingMode)?.label ||
        enabledStatus;

  const mutationStatus = isMutationEnabled
    ? MUTATION_MODES.find((m) => m.value === mutation.mode)?.label ||
      enabledStatus
    : disabledStatus;

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
        hasChanges={hasSubChanges(FAKING_ACCORDIONS[0].prefixes)}
      >
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Switch
              label={t("sets.faking.fakeSni.enable")}
              checked={config.faking.sni}
              path="faking.sni"
              onChange={(checked: boolean) => onChange("faking.sni", checked)}
              description={t("sets.faking.fakeSni.enableDesc")}
              info={t("sets.faking.fakeSni.enableInfo")}
              learnMore="sets/tcp/faking"
              aiTopic="faking.sni"
            />
          </Grid>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Slider
              label={t("sets.faking.fakeSni.packetCount")}
              value={config.faking.sni_seq_length}
              path="faking.sni_seq_length"
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

          <B4FormHeader label={t("sets.faking.fakeSni.payloadSection")} />

          <Grid size={{ xs: 12, md: 6 }}>
            <Stack spacing={2}>
              <B4Select
                label={t("sets.faking.fakeSni.payloadType")}
                value={config.faking.sni_type}
                options={FAKE_PAYLOAD_TYPES}
                path="faking.sni_type"
                onChange={(e) =>
                  onChange("faking.sni_type", Number(e.target.value))
                }
                helperText={t(
                  {
                    [FakingPayloadType.RANDOM]:
                      "sets.faking.fakeSni.payloadRandomNote",
                    [FakingPayloadType.CUSTOM]:
                      "sets.faking.fakeSni.payloadCustomNote",
                    [FakingPayloadType.DEFAULT]:
                      "sets.faking.fakeSni.payloadPresetNote",
                    [FakingPayloadType.DEFAULT2]:
                      "sets.faking.fakeSni.payloadPresetNote",
                    [FakingPayloadType.CAPTURE]:
                      "sets.faking.fakeSni.payloadFileNote",
                    [FakingPayloadType.ZERO]:
                      "sets.faking.fakeSni.payloadZerosNote",
                    [FakingPayloadType.INVERTED]:
                      "sets.faking.fakeSni.payloadInvertedNote",
                    [FakingPayloadType.DOMAIN]:
                      "sets.faking.fakeSni.payloadDomainNote",
                    [FakingPayloadType.STUN]:
                      "sets.faking.fakeSni.payloadStunNote",
                  }[config.faking.sni_type],
                )}
                learnMore="sets/tcp/faking#fake-payload"
                disabled={!config.faking.sni}
                aiTopic="faking.sni_type"
                aiContext={{
                  available: FAKE_PAYLOAD_TYPES.map((p) => p.value),
                }}
              />

              {config.faking.sni_type === FakingPayloadType.CUSTOM && (
                <B4TextField
                  value={config.faking.custom_payload}
                  path="faking.custom_payload"
                  onChange={(e) =>
                    onChange("faking.custom_payload", e.target.value)
                  }
                  helperText={t("sets.faking.fakeSni.customPayloadHelper")}
                  disabled={!config.faking.sni}
                  multiline
                  rows={2}
                />
              )}
              {config.faking.sni_type === FakingPayloadType.DOMAIN && (
                <B4TextField
                  label={t("sets.faking.fakeSni.payloadDomainLabel")}
                  value={config.faking.payload_domain}
                  path="faking.payload_domain"
                  onChange={(e) =>
                    onChange("faking.payload_domain", e.target.value)
                  }
                  helperText={t("sets.faking.fakeSni.payloadDomainHelper")}
                  message={
                    payloadDomainNotGiven ? (
                      <B4FieldMessage>
                        {t("sets.faking.fakeSni.payloadDomainNotGiven")}
                      </B4FieldMessage>
                    ) : undefined
                  }
                  disabled={!config.faking.sni}
                  placeholder="example.com"
                />
              )}
              {config.faking.sni_type === FakingPayloadType.CAPTURE && (
                <B4Select
                  label={t("sets.faking.fakeSni.generatedPayload")}
                  value={payloadFile}
                  path="faking.payload_file"
                  options={payloadFileOptions}
                  onChange={(e) =>
                    onChange("faking.payload_file", e.target.value)
                  }
                  helperText={t("sets.faking.fakeSni.selectPayloadHelper")}
                  message={
                    payloadFileMessages.length > 0 ? (
                      <>{payloadFileMessages}</>
                    ) : undefined
                  }
                  info={payloadFileInfo}
                  learnMore="settings/payloads#using-in-sets"
                  disabled={!config.faking.sni}
                />
              )}
            </Stack>
          </Grid>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Switch
              label={t("sets.faking.fakeSni.matchLength")}
              description={t("sets.faking.fakeSni.matchLengthDesc")}
              info={t("sets.faking.fakeSni.matchLengthInfo")}
              learnMore="sets/tcp/faking#fake-payload"
              checked={config.faking.fake_len_mode === "match"}
              path="faking.fake_len_mode"
              onChange={(checked) =>
                onChange("faking.fake_len_mode", checked ? "match" : "")
              }
              disabled={!config.faking.sni}
              aiTopic="faking.fake_len_mode"
            />
          </Grid>
          {tlsModChangesTheFake(config, chosenFile) && (
            <Grid size={{ xs: 12 }}>
              <Typography variant="subtitle2">
                {t("sets.faking.fakeSni.tlsModTitle")}
              </Typography>
              <Typography
                variant="caption"
                color="text.secondary"
                sx={{ display: "block", mb: 1.5 }}
              >
                {t("sets.faking.fakeSni.tlsModDesc")}
              </Typography>
              <Grid container spacing={2}>
                <Grid size={{ xs: 12, md: 6 }}>
                  <B4Switch
                    label={t("sets.faking.fakeSni.randomizeTlsRandom")}
                    checked={(config.faking.tls_mod || []).includes("rnd")}
                    path="faking.tls_mod"
                    arrayItem="rnd"
                    onChange={(checked: boolean) => {
                      const current = config.faking.tls_mod || [];
                      const next = checked
                        ? [...current.filter((m) => m !== "rnd"), "rnd"]
                        : current.filter((m) => m !== "rnd");
                      onChange("faking.tls_mod", next);
                    }}
                    description={t(
                      "sets.faking.fakeSni.randomizeTlsRandomDesc",
                    )}
                    info={t("sets.faking.fakeSni.randomizeTlsRandomInfo")}
                    learnMore="sets/tcp/faking#fake-payload"
                    disabled={!config.faking.sni}
                    aiTopic="faking.tls_mod"
                  />
                </Grid>
                <Grid size={{ xs: 12, md: 6 }}>
                  <B4Switch
                    label={t("sets.faking.fakeSni.dupSessionId")}
                    checked={(config.faking.tls_mod || []).includes("dupsid")}
                    path="faking.tls_mod"
                    arrayItem="dupsid"
                    onChange={(checked: boolean) => {
                      const current = config.faking.tls_mod || [];
                      const next = checked
                        ? [...current.filter((m) => m !== "dupsid"), "dupsid"]
                        : current.filter((m) => m !== "dupsid");
                      onChange("faking.tls_mod", next);
                    }}
                    description={t("sets.faking.fakeSni.dupSessionIdDesc")}
                    info={t("sets.faking.fakeSni.dupSessionIdInfo")}
                    learnMore="sets/tcp/faking#fake-payload"
                    disabled={!config.faking.sni}
                    aiTopic="faking.tls_mod"
                  />
                </Grid>
              </Grid>
            </Grid>
          )}

          <B4FormHeader label={t("sets.faking.fakeSni.rejectionSection")} />

          <Grid size={{ xs: 12, md: 6 }}>
            <B4Select
              label={t("sets.faking.fakeSni.strategy")}
              value={config.faking.strategy}
              options={FAKE_STRATEGIES}
              path="faking.strategy"
              onChange={(e) => onChange("faking.strategy", e.target.value)}
              helperText={t(
                {
                  ttl: "sets.faking.fakeSni.strategyTtlNote",
                  randseq: "sets.faking.fakeSni.strategyRandseqNote",
                  pastseq: "sets.faking.fakeSni.strategyPastseqNote",
                  tcp_check: "sets.faking.fakeSni.strategyTcpCheckNote",
                  md5sum: "sets.faking.fakeSni.strategyMd5sumNote",
                  timestamp: "sets.faking.fakeSni.strategyTimestampNote",
                }[config.faking.strategy],
              )}
              info={t("sets.faking.fakeSni.strategyInfo")}
              learnMore="sets/tcp/faking"
              disabled={!config.faking.sni}
              aiTopic="faking.strategy"
              aiContext={{ available: FAKE_STRATEGIES.map((s) => s.value) }}
            />
          </Grid>
          {(config.faking.strategy === "pastseq" ||
            config.faking.strategy === "randseq") && (
            <Grid size={{ xs: 12, md: 6 }}>
              <B4NumberField
                value={config.faking.seq_offset}
                path="faking.seq_offset"
                onChange={(n) => onChange("faking.seq_offset", n)}
                helperText={t("sets.faking.fakeSni.seqOffsetHelper")}
                disabled={!config.faking.sni}
              />
            </Grid>
          )}
          {config.faking.strategy === "timestamp" && (
            <Grid size={{ xs: 12, md: 6 }}>
              <B4NumberField
                value={config.faking.timestamp_decrease || 600000}
                path="faking.timestamp_decrease"
                onChange={(n) => onChange("faking.timestamp_decrease", n)}
                min={0}
                helperText={t("sets.faking.fakeSni.timestampDecreaseHelper")}
                info={t("sets.faking.fakeSni.timestampDecreaseInfo")}
                disabled={!config.faking.sni}
              />
            </Grid>
          )}
        </Grid>
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Slider
              value={config.faking.ttl}
              path="faking.ttl"
              onChange={(value: number) => onChange("faking.ttl", value)}
              min={1}
              max={64}
              step={1}
              helperText={t(
                isFakeTtlActive
                  ? "sets.faking.fakeSni.ttlHelper"
                  : "sets.faking.fakeSni.ttlUnused",
              )}
              info={t("sets.faking.fakeSni.ttlInfo")}
              learnMore="sets/tcp/faking"
              disabled={!isFakeTtlActive}
            />
          </Grid>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Switch
              description={t("sets.faking.fakeSni.applyTtlDesc")}
              info={t("sets.faking.fakeSni.applyTtlInfo")}
              learnMore="sets/tcp/faking"
              checked={config.faking.apply_ttl || false}
              path="faking.apply_ttl"
              onChange={(checked) => onChange("faking.apply_ttl", checked)}
              disabled={!config.faking.sni || config.faking.strategy === "ttl"}
              aiTopic="faking.apply_ttl"
            />
          </Grid>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Switch
              description={t("sets.faking.fakeSni.md5OnFakeDesc")}
              info={t("sets.faking.fakeSni.md5OnFakeInfo")}
              learnMore="sets/tcp/faking"
              checked={config.faking.md5_on_fake || false}
              path="faking.md5_on_fake"
              onChange={(checked) => onChange("faking.md5_on_fake", checked)}
              disabled={!config.faking.sni}
              aiTopic="faking.md5_on_fake"
            />
          </Grid>
        </Grid>
      </B4Accordion>

      {/* SYN Fake Packets */}
      <B4Accordion
        title={t("sets.faking.synFake.title")}
        status={synFakeStatus}
        enabled={config.tcp.syn_fake || config.faking.tcp_md5}
        hasChanges={hasSubChanges(FAKING_ACCORDIONS[1].prefixes)}
      >
        <B4Switch
          label={t("sets.faking.synFake.enable")}
          description={t("sets.faking.synFake.enableDesc")}
          info={t("sets.faking.synFake.enableInfo")}
          learnMore="sets/tcp/faking"
          checked={config.tcp.syn_fake || false}
          path="tcp.syn_fake"
          onChange={(checked) => onChange("tcp.syn_fake", checked)}
          aiTopic="tcp.syn_fake"
        />
        {config.tcp.syn_fake && (
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }}>
              <B4Slider
                label={t("sets.faking.synFake.payloadLen")}
                value={config.tcp.syn_fake_len || 0}
                path="tcp.syn_fake_len"
                onChange={(value: number) =>
                  onChange("tcp.syn_fake_len", value)
                }
                min={0}
                max={1200}
                step={64}
                valueSuffix={t("core.units.bytes")}
                helperText={t("sets.faking.synFake.payloadLenHelper")}
              />
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <B4Slider
                label={t("sets.faking.synFake.ttl")}
                value={config.tcp.syn_ttl || 0}
                path="tcp.syn_ttl"
                onChange={(value: number) => onChange("tcp.syn_ttl", value)}
                min={1}
                max={100}
                step={1}
                helperText={t("sets.faking.synFake.ttlHelper")}
                info={t("sets.faking.synFake.ttlInfo")}
              />
            </Grid>
          </Grid>
        )}
        <B4Switch
          label={t("sets.faking.synFake.md5Enable")}
          description={t("sets.faking.synFake.md5Desc")}
          info={t("sets.faking.synFake.md5Info")}
          learnMore="sets/tcp/faking"
          checked={config.faking.tcp_md5 || false}
          path="faking.tcp_md5"
          onChange={(checked) => onChange("faking.tcp_md5", checked)}
          aiTopic="faking.tcp_md5"
        />
      </B4Accordion>

      {/* TCP Desync Configuration */}
      <B4Accordion
        title={t("sets.faking.desync.title")}
        status={desyncStatus}
        enabled={isDesyncEnabled}
        hasChanges={hasSubChanges(FAKING_ACCORDIONS[2].prefixes)}
      >
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Select
              value={config.tcp.desync.mode}
              options={desyncModeOptions}
              path="tcp.desync.mode"
              onChange={(e) => onChange("tcp.desync.mode", e.target.value)}
              helperText={t(
                {
                  off: "sets.faking.desync.descOff",
                  rst: "sets.faking.desync.descRst",
                  fin: "sets.faking.desync.descFin",
                  ack: "sets.faking.desync.descAck",
                  combo: "sets.faking.desync.descCombo",
                  full: "sets.faking.desync.descFull",
                }[config.tcp.desync.mode],
              )}
              info={t("sets.faking.desync.modeInfo")}
              learnMore="sets/tcp/faking#tcp-desync-attack"
              aiTopic="tcp.desync.mode"
              aiContext={{ available: desyncModeOptions.map((o) => o.value) }}
            />
          </Grid>
        </Grid>
        {isDesyncEnabled && (
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }}>
              <B4Slider
                value={config.tcp.desync.ttl}
                path="tcp.desync.ttl"
                onChange={(value: number) => onChange("tcp.desync.ttl", value)}
                min={1}
                max={50}
                step={1}
                helperText={t("sets.faking.desync.ttlHelperEnabled")}
              />
            </Grid>
            {desyncCountUsed && (
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Slider
                  value={config.tcp.desync.count}
                  path="tcp.desync.count"
                  onChange={(value: number) =>
                    onChange("tcp.desync.count", value)
                  }
                  min={1}
                  max={20}
                  step={1}
                  helperText={t("sets.faking.desync.countHelperEnabled")}
                />
              </Grid>
            )}
          </Grid>
        )}
        <B4Switch
          description={t("sets.faking.desync.postDesyncDesc")}
          info={t("sets.faking.desync.postDesyncInfo")}
          learnMore="sets/tcp/faking#tcp-desync-attack"
          checked={config.tcp.desync.post_desync || false}
          path="tcp.desync.post_desync"
          onChange={(checked) => onChange("tcp.desync.post_desync", checked)}
          aiTopic="tcp.desync.post_desync"
        />
      </B4Accordion>

      {/* TCP Window Manipulation */}
      <B4Accordion
        title={t("sets.faking.window.title")}
        status={windowStatus}
        enabled={config.tcp.win.mode !== "off"}
        hasChanges={hasSubChanges(FAKING_ACCORDIONS[3].prefixes)}
      >
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Select
              value={config.tcp.win.mode}
              options={windowModeOptions}
              path="tcp.win.mode"
              onChange={(e) => onChange("tcp.win.mode", e.target.value)}
              helperText={t(
                {
                  off: "sets.faking.window.descOff",
                  zero: "sets.faking.window.descZero",
                  random: "sets.faking.window.descRandom",
                  oscillate: "sets.faking.window.descOscillate",
                  escalate: "sets.faking.window.descEscalate",
                }[config.tcp.win.mode],
              )}
              info={t("sets.faking.window.modeInfo")}
              learnMore="sets/tcp/faking"
              aiTopic="tcp.win.mode"
              aiContext={{ available: windowModeOptions.map((o) => o.value) }}
            />
          </Grid>
        </Grid>
        {showWinValues && (
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }}>
              <Box sx={{ display: "flex", gap: 1, alignItems: "flex-start" }}>
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
                  helperText={t(
                    config.tcp.win.mode === "oscillate"
                      ? "sets.faking.window.oscillateHint"
                      : "sets.faking.window.randomHint",
                  )}
                  message={
                    winValues.length === 0 ? (
                      <B4FieldMessage severity="info">
                        {t("sets.faking.window.emptyValues")}
                      </B4FieldMessage>
                    ) : undefined
                  }
                />
                <B4PlusButton
                  onClick={handleAddWinValue}
                  disabled={!newWinValue}
                />
              </Box>
            </Grid>
            <B4ChipList
              items={winValues}
              getKey={(v) => v}
              getLabel={(v) => v.toLocaleString()}
              onDelete={handleRemoveWinValue}
              title={t("sets.faking.window.customValues")}
              gridSize={{ xs: 12, md: 6 }}
            />
          </Grid>
        )}
      </B4Accordion>

      {/* Incoming Response Bypass */}
      <B4Accordion
        title={t("sets.faking.incoming.title")}
        status={incomingStatus}
        enabled={incomingMode !== "off"}
        hasChanges={hasSubChanges(FAKING_ACCORDIONS[4].prefixes)}
      >
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Select
              value={incomingMode}
              options={incomingModeOptions}
              path="tcp.incoming.mode"
              onChange={(e) => onChange("tcp.incoming.mode", e.target.value)}
              helperText={t(
                {
                  off: "sets.faking.incoming.descOff",
                  fake: "sets.faking.incoming.descFake",
                  reset: "sets.faking.incoming.descReset",
                  fin: "sets.faking.incoming.descFin",
                  desync: "sets.faking.incoming.descDesync",
                }[incomingMode],
              )}
              info={t("sets.faking.incoming.modeInfo")}
              learnMore="sets/tcp/faking"
              aiTopic="tcp.incoming.mode"
              aiContext={{ available: incomingModeOptions.map((o) => o.value) }}
            />
          </Grid>
          {incomingMode === "fake" && (
            <Grid size={{ xs: 12, md: 6 }}>
              <B4Select
                value={incomingStrategy}
                options={incomingStrategyOptions}
                path="tcp.incoming.strategy"
                onChange={(e) =>
                  onChange("tcp.incoming.strategy", e.target.value)
                }
                helperText={t(
                  {
                    badsum: "sets.faking.incoming.stratDescBadsum",
                    badseq: "sets.faking.incoming.stratDescBadseq",
                    badack: "sets.faking.incoming.stratDescBadack",
                    rand: "sets.faking.incoming.stratDescRand",
                    all: "sets.faking.incoming.stratDescAll",
                  }[incomingStrategy],
                )}
                info={t("sets.faking.incoming.strategyInfo")}
                learnMore="sets/tcp/faking"
                aiTopic="tcp.incoming.strategy"
                aiContext={{
                  available: incomingStrategyOptions.map((o) => o.value),
                }}
              />
            </Grid>
          )}
        </Grid>
        {incomingMode !== "off" && (
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }}>
              <B4Slider
                label={t("sets.faking.incoming.ttl")}
                value={config.tcp.incoming?.fake_ttl || 3}
                path="tcp.incoming.fake_ttl"
                onChange={(value: number) =>
                  onChange("tcp.incoming.fake_ttl", value)
                }
                min={1}
                max={20}
                step={1}
                helperText={t(
                  incomingMode === "fake"
                    ? "sets.faking.incoming.ttlHelper"
                    : "sets.faking.incoming.ttlHelperAlone",
                )}
              />
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <B4Slider
                value={config.tcp.incoming?.fake_count || 3}
                path="tcp.incoming.fake_count"
                onChange={(value: number) =>
                  onChange("tcp.incoming.fake_count", value)
                }
                min={1}
                max={10}
                step={1}
                helperText={t("sets.faking.incoming.fakeCountHelper")}
              />
            </Grid>
            {incomingMode !== "fake" && (
              <>
                <Grid size={{ xs: 12, md: 6 }}>
                  <B4Slider
                    label={t("sets.faking.incoming.thresholdMin")}
                    value={config.tcp.incoming?.min || 14}
                    path="tcp.incoming.min"
                    onChange={(value: number) =>
                      onChange("tcp.incoming.min", value)
                    }
                    min={5}
                    max={config.tcp.incoming?.max || 150}
                    step={1}
                    valueSuffix={t("core.units.kb")}
                    helperText={t("sets.faking.incoming.thresholdMinHelper")}
                    info={t("sets.faking.incoming.thresholdInfo")}
                    aiTopic="tcp.incoming.min"
                  />
                </Grid>
                <Grid size={{ xs: 12, md: 6 }}>
                  <B4Slider
                    label={t("sets.faking.incoming.thresholdMax")}
                    value={config.tcp.incoming?.max || 14}
                    path="tcp.incoming.max"
                    onChange={(value: number) =>
                      onChange("tcp.incoming.max", value)
                    }
                    min={config.tcp.incoming?.min || 5}
                    max={50}
                    step={1}
                    valueSuffix={t("core.units.kb")}
                    helperText={t(
                      config.tcp.incoming?.min === config.tcp.incoming?.max
                        ? "sets.faking.incoming.thresholdMaxFixed"
                        : "sets.faking.incoming.thresholdMaxHelper",
                    )}
                    info={t("sets.faking.incoming.thresholdInfo")}
                    aiTopic="tcp.incoming.max"
                  />
                </Grid>
              </>
            )}
          </Grid>
        )}
      </B4Accordion>

      {/* ClientHello Mutation Section */}
      <B4Accordion
        title={t("sets.faking.mutation.title")}
        status={mutationStatus}
        enabled={isMutationEnabled}
        hasChanges={hasSubChanges(FAKING_ACCORDIONS[5].prefixes)}
      >
        <Grid container spacing={2}>
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Select
              value={mutation.mode}
              options={MUTATION_MODES}
              path="faking.sni_mutation.mode"
              onChange={(e) =>
                onChange("faking.sni_mutation.mode", e.target.value)
              }
              helperText={t(
                {
                  off: "sets.faking.mutation.descOff",
                  random: "sets.faking.mutation.descRandom",
                  grease: "sets.faking.mutation.descGrease",
                  padding: "sets.faking.mutation.descPadding",
                  fakeext: "sets.faking.mutation.descFakeext",
                  fakesni: "sets.faking.mutation.descFakesni",
                  advanced: "sets.faking.mutation.descAdvanced",
                }[mutation.mode],
              )}
              learnMore="sets/tcp/faking"
              aiTopic="faking.sni_mutation.mode"
              aiContext={{ available: MUTATION_MODES.map((m) => m.value) }}
            />
          </Grid>
        </Grid>
        {(showGreaseSettings || showPaddingSettings || showFakeExtSettings) && (
          <Grid container spacing={2}>
            {showGreaseSettings && (
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Slider
                  label={t("sets.faking.mutation.greaseCount")}
                  value={mutation.grease_count}
                  path="faking.sni_mutation.grease_count"
                  onChange={(value: number) =>
                    onChange("faking.sni_mutation.grease_count", value)
                  }
                  min={1}
                  max={10}
                  step={1}
                  helperText={t("sets.faking.mutation.greaseCountHelper")}
                />
              </Grid>
            )}
            {showPaddingSettings && (
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Slider
                  label={t("sets.faking.mutation.paddingSize")}
                  value={mutation.padding_size}
                  path="faking.sni_mutation.padding_size"
                  onChange={(value: number) =>
                    onChange("faking.sni_mutation.padding_size", value)
                  }
                  min={256}
                  max={16384}
                  step={256}
                  valueSuffix={t("core.units.bytes")}
                  helperText={t("sets.faking.mutation.paddingSizeHelper")}
                />
              </Grid>
            )}
            {showFakeExtSettings && (
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Slider
                  label={t("sets.faking.mutation.fakeExtCount")}
                  value={mutation.fake_ext_count}
                  path="faking.sni_mutation.fake_ext_count"
                  onChange={(value: number) =>
                    onChange("faking.sni_mutation.fake_ext_count", value)
                  }
                  min={1}
                  max={15}
                  step={1}
                  helperText={t("sets.faking.mutation.fakeExtCountHelper")}
                />
              </Grid>
            )}
          </Grid>
        )}
        {showFakeSniSettings && (
          <Grid container spacing={2}>
            <Grid size={{ xs: 12, md: 6 }}>
              <Box sx={{ display: "flex", gap: 1, alignItems: "flex-start" }}>
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
                  placeholder={t("sets.faking.mutation.fakeSniPlaceholder")}
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
          </Grid>
        )}
      </B4Accordion>
    </Stack>
  );
};
