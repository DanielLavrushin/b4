import { useMemo } from "react";
import { Grid, Link } from "@mui/material";
import { B4SetConfig, MSSClampConfig, WSSizeConfig } from "@models/config";
import { createDefaultSet } from "@models/defaults";
import { OpenInNewIcon } from "@b4.icons";
import {
  B4Advanced,
  B4FieldMessage,
  B4Slider,
  B4Select,
  B4RangeSlider,
  B4TextField,
  B4NumberField,
  B4FormHeader,
} from "@b4.elements";
import { B4Switch } from "@common/B4Switch";
import {
  declaresIpTargets,
  effectiveQueueMark,
  isWSSizeReuseRefusal,
  mssClampIpScope,
  portFilterCovers,
  reuseTunEngine,
  wssizeReuseIssues,
} from "@utils";
import { useTunEngine } from "@hooks/useEngineMode";
import { useTranslation } from "react-i18next";

interface TcpGeneralProps {
  config: B4SetConfig;
  queueMode?: string;
  skipSetup?: boolean;
  queueMark?: number;
  onChange: (
    field: string,
    value: string | number | boolean | number[],
  ) => void;
}

const IBD_FALLBACK = {
  action: "block",
  enabled: false,
  retransmit_threshold: 3,
  timeout_ms: 3000,
  cache_blocked_ips: true,
  syn_detect: true,
  syn_threshold: 3,
  heal_dns: false,
  heal_ttl_sec: 60,
};

const IBD_ADVANCED_PATHS = [
  "tcp.ip_block_detect.syn_threshold",
  "tcp.ip_block_detect.retransmit_threshold",
  "tcp.ip_block_detect.timeout_ms",
  "tcp.ip_block_detect.heal_ttl_sec",
];

function wssizeScaleMode(ws: WSSizeConfig): "keep" | "lower" | "strip" {
  if (ws.strip_scale) return "strip";
  return ws.scale > 0 ? "lower" : "keep";
}

export const TcpGeneral = ({
  config,
  queueMode,
  skipSetup = false,
  queueMark,
  onChange,
}: TcpGeneralProps) => {
  const { t } = useTranslation();
  const tunEngine = queueMode === "tun";
  const tunRunning = useTunEngine();
  const ws: WSSizeConfig = {
    enabled: false,
    window: 0,
    scale: 0,
    cutoff: "",
    strip_scale: false,
    ...config.tcp.wssize,
  };
  const reuse = config.tcp.wssize_reuse ?? {
    enabled: false,
    max_age_seconds: 300,
  };
  const reuseIssues = wssizeReuseIssues(config, {
    tunEngine: reuseTunEngine(queueMode, tunRunning),
    skipSetup,
    queueMark,
  });
  const dup = config.tcp.duplicate ?? { enabled: false, count: 3 };
  const wsConflict = (config.routing?.enabled ?? false) || dup.enabled;
  const wsTun = ws.enabled && !wsConflict && tunEngine;
  const wsScaleMode = wssizeScaleMode(ws);
  const setWsScaleMode = (mode: string) => {
    onChange("tcp.wssize.strip_scale", mode === "strip");
    onChange("tcp.wssize.scale", mode === "lower" ? Math.max(ws.scale, 1) : 0);
  };
  const wsIdle =
    ws.enabled &&
    !wsConflict &&
    !tunEngine &&
    ws.window === 0 &&
    wsScaleMode === "keep";
  const ibd = { ...IBD_FALLBACK, ...config.tcp.ip_block_detect };
  const ibdDefaults = useMemo(
    () => ({ ...IBD_FALLBACK, ...createDefaultSet(0).tcp.ip_block_detect }),
    [],
  );
  const ibdCollects = ibd.action === "collect";
  const ibdCustomized = [
    !ibdCollects &&
      ibd.syn_detect &&
      ibd.syn_threshold !== ibdDefaults.syn_threshold,
    !ibdCollects &&
      ibd.retransmit_threshold !== ibdDefaults.retransmit_threshold,
    ibd.timeout_ms !== ibdDefaults.timeout_ms,
    !ibdCollects &&
      ibd.heal_dns &&
      ibd.heal_ttl_sec !== ibdDefaults.heal_ttl_sec,
  ].filter(Boolean).length;
  const desync = config.tcp.desync;
  const collectFakesClose =
    ibdCollects &&
    (desync.post_desync || (desync.mode !== "off" && desync.mode !== "ack"));
  const rstProt = {
    enabled: false,
    ttl_tolerance: 3,
    ...config.tcp.rst_protection,
  };
  const mss: MSSClampConfig = config.mss_clamp ?? { enabled: false, size: 88 };
  const declaresIPs = declaresIpTargets(config.targets);
  const hasIPScope = mssClampIpScope(config.targets);
  const hasMACScope = (config.targets?.source_devices?.length ?? 0) > 0;
  const mssScopeOk = hasIPScope || hasMACScope;
  const hasDomainTargets =
    (config.targets?.sni_domains?.length ?? 0) > 0 ||
    (config.targets?.geosite_categories?.length ?? 0) > 0;
  const hasDestinationTargets = declaresIPs || hasDomainTargets;
  const wsDomainOnly =
    ws.enabled &&
    !wsConflict &&
    !wsTun &&
    (config.targets?.domain_only ?? false) &&
    hasDestinationTargets &&
    !declaresIPs;
  const wsFirstPacketOnly =
    ws.enabled &&
    !wsConflict &&
    !wsTun &&
    !wsDomainOnly &&
    hasDomainTargets &&
    !declaresIPs;
  const hasPortFilter = (config.tcp.dport_filter ?? "").trim() !== "";
  const portFilterSkipsHttps =
    hasDestinationTargets &&
    hasPortFilter &&
    !portFilterCovers(config.tcp.dport_filter, 443);
  const portFilterBlocksL7 =
    hasPortFilter && (config.udp.filter_l7 ?? []).length > 0;

  const portFilterMessages = [
    portFilterSkipsHttps && (
      <B4FieldMessage key="https">
        {t("sets.tcp.general.portFilterNoHttps")}
      </B4FieldMessage>
    ),
    portFilterBlocksL7 && (
      <B4FieldMessage key="calls">
        {t("sets.tcp.general.portFilterL7")}
      </B4FieldMessage>
    ),
  ].filter(Boolean);

  const wsMessages = [
    wsConflict && (
      <B4FieldMessage key="conflict">
        {t("sets.tcp.general.wssizeConflict")}
      </B4FieldMessage>
    ),
    wsTun && (
      <B4FieldMessage key="tun">{t("sets.tcp.general.wssizeTun")}</B4FieldMessage>
    ),
    wsDomainOnly && (
      <B4FieldMessage key="domain-only">
        {t("sets.tcp.general.wssizeDomainOnly")}
      </B4FieldMessage>
    ),
    wsFirstPacketOnly && (
      <B4FieldMessage key="first-packet" severity="info">
        {t("sets.tcp.general.wssizeFirstPacket")}
      </B4FieldMessage>
    ),
    wsIdle && (
      <B4FieldMessage key="idle" severity="info">
        {t("sets.tcp.general.wssizeIdle")}
      </B4FieldMessage>
    ),
  ].filter(Boolean);

  const reuseMessages = reuseIssues.map((issue) => (
    <B4FieldMessage
      key={issue}
      severity={isWSSizeReuseRefusal(issue) ? "error" : "warning"}
    >
      {t(`sets.tcp.general.wssizeReuseIssue.${issue}`, {
        mark: `0x${effectiveQueueMark(queueMark).toString(16)}`,
      })}
    </B4FieldMessage>
  ));

  const mssMessage = !mssScopeOk ? (
    <B4FieldMessage>{t("sets.tcp.general.mssScopeRequired")}</B4FieldMessage>
  ) : mss.enabled ? (
    <B4FieldMessage severity="info">
      {t("sets.tcp.general.mssAppliesTo", {
        ips: hasIPScope ? "✓" : "-",
        macs: hasMACScope ? "✓" : "-",
      })}
    </B4FieldMessage>
  ) : undefined;

  return (
    <>
      <Grid container spacing={3} sx={{ mt: 1, mb: 3 }}>
        <Grid size={{ xs: 12, md: 6 }}>
          <B4RangeSlider
            value={[
              config.tcp.seg2delay,
              config.tcp.seg2delay_max || config.tcp.seg2delay,
            ]}
            path="tcp.seg2delay"
            onChange={(value: [number, number]) => {
              onChange("tcp.seg2delay", value[0]);
              onChange("tcp.seg2delay_max", value[1]);
            }}
            min={0}
            max={1000}
            step={10}
            valueSuffix={t("core.units.ms")}
            helperText={t("sets.tcp.general.seg2delayHelper")}
            aiTopic="tcp.seg2delay"
          />
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <B4TextField
            label={t("sets.tcp.general.portFilter")}
            value={config.tcp.dport_filter}
            path="tcp.dport_filter"
            onChange={(e) => onChange("tcp.dport_filter", e.target.value)}
            placeholder={t("sets.tcp.general.portFilterPlaceholder")}
            helperText={t("sets.tcp.general.portFilterHelper")}
            message={
              portFilterMessages.length > 0 ? <>{portFilterMessages}</> : undefined
            }
            info={t("sets.tcp.general.portFilterInfo")}
            aiTopic="tcp.dport_filter"
          />
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <B4Switch
            description={t("sets.tcp.general.dropSackDesc")}
            checked={config.tcp.drop_sack || false}
            path="tcp.drop_sack"
            onChange={(checked) => onChange("tcp.drop_sack", checked)}
            aiTopic="tcp.drop_sack"
          />
        </Grid>

        <Grid size={{ xs: 12, md: 6 }}>
          <B4Switch
            label={t("sets.tcp.general.httpMethodEol")}
            description={t("sets.tcp.general.httpMethodEolDesc")}
            info={t("sets.tcp.general.httpMethodEolInfo")}
            checked={config.tcp.http_methodeol || false}
            path="tcp.http_methodeol"
            onChange={(checked) => onChange("tcp.http_methodeol", checked)}
            aiTopic="tcp.http_methodeol"
          />
        </Grid>
      </Grid>

      <B4FormHeader label={t("sets.tcp.general.wssize")} />
      <Grid container spacing={3} mb={3} mt={2}>
        <Grid size={{ xs: 12 }}>
          <B4Switch
            label={t("sets.tcp.general.wssizeEnable")}
            description={t("sets.tcp.general.wssizeEnableDesc")}
            message={wsMessages.length > 0 ? <>{wsMessages}</> : undefined}
            info={t(
              reuse.enabled
                ? "sets.tcp.general.wssizeReuseAlert"
                : "sets.tcp.general.wssizeInfo",
            )}
            learnMore="sets/tcp/general#wssize"
            credit={
              <>
                {t("sets.tcp.general.wssizeCredit")}{" "}
                <Link
                  href="https://github.com/bol-van/zapret2"
                  target="_blank"
                  rel="noreferrer"
                  sx={{ display: "inline-flex", alignItems: "center", gap: 0.25 }}
                >
                  zapret2
                  <OpenInNewIcon sx={{ fontSize: 12 }} />
                </Link>
              </>
            }
            checked={ws.enabled}
            path="tcp.wssize.enabled"
            onChange={(checked) => {
              onChange("tcp.wssize.enabled", checked);
              if (!checked) onChange("tcp.wssize_reuse.enabled", false);
            }}
            disabled={wsConflict && !ws.enabled}
            aiTopic="tcp.wssize.enabled"
            aiContext={{
              window: ws.window,
              scale: ws.scale,
              strip_scale: ws.strip_scale ?? false,
              cutoff: ws.cutoff ?? "",
            }}
          />
        </Grid>
        {ws.enabled && (
          <>
            <Grid size={{ xs: 12, md: 6 }}>
              <B4NumberField
                label={t("sets.tcp.general.wssizeWindow")}
                value={ws.window}
                path="tcp.wssize.window"
                onChange={(value) => onChange("tcp.wssize.window", value)}
                min={0}
                max={65535}
                helperText={t("sets.tcp.general.wssizeWindowHelper")}
                info={t("sets.tcp.general.wssizeWindowInfo")}
                aiTopic="tcp.wssize.window"
              />
            </Grid>
            <Grid size={{ xs: 12, md: 6 }}>
              <B4Select
                label={t("sets.tcp.general.wssizeScaleMode")}
                value={wsScaleMode}
                path="tcp.wssize.strip_scale"
                options={[
                  {
                    value: "keep",
                    label: t("sets.tcp.general.wssizeScaleKeep"),
                    note: t("sets.tcp.general.wssizeScaleKeepNote"),
                  },
                  {
                    value: "lower",
                    label: t("sets.tcp.general.wssizeScaleLower"),
                    note: t("sets.tcp.general.wssizeScaleLowerNote"),
                  },
                  {
                    value: "strip",
                    label: t("sets.tcp.general.wssizeScaleStrip"),
                    note: t("sets.tcp.general.wssizeScaleStripNote"),
                  },
                ]}
                onChange={(e) => setWsScaleMode(String(e.target.value))}
                helperText={t("sets.tcp.general.wssizeScaleModeHelper")}
                changeLabel={t("sets.tcp.general.wssizeStripScale")}
                learnMore="sets/tcp/general#wssize"
                aiTopic="tcp.wssize.strip_scale"
                aiContext={{
                  strip_scale: ws.strip_scale ?? false,
                  scale: ws.scale,
                  window: ws.window,
                }}
              />
            </Grid>
            {wsScaleMode === "lower" && (
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Slider
                  label={t("sets.tcp.general.wssizeScale")}
                  value={ws.scale}
                  path="tcp.wssize.scale"
                  onChange={(value: number) =>
                    onChange("tcp.wssize.scale", value)
                  }
                  min={1}
                  max={14}
                  step={1}
                  helperText={t("sets.tcp.general.wssizeScaleHelper")}
                  aiTopic="tcp.wssize.scale"
                />
              </Grid>
            )}
            {ws.window > 0 && (
              <Grid size={{ xs: 12, md: 6 }}>
                <B4Select
                  label={t("sets.tcp.general.wssizeCutoff")}
                  value={ws.cutoff ?? ""}
                  path="tcp.wssize.cutoff"
                  options={[
                    {
                      value: "",
                      label: t("sets.tcp.general.wssizeCutoffHello"),
                      note: t("sets.tcp.general.wssizeCutoffHelloNote"),
                    },
                    {
                      value: "first_data",
                      label: t("sets.tcp.general.wssizeCutoffFirst"),
                      note: t("sets.tcp.general.wssizeCutoffFirstNote"),
                    },
                  ]}
                  onChange={(e) =>
                    onChange("tcp.wssize.cutoff", e.target.value)
                  }
                  info={t("sets.tcp.general.wssizeCutoffInfo")}
                  aiTopic="tcp.wssize.cutoff"
                />
              </Grid>
            )}
          </>
        )}
        {(ws.enabled || reuse.enabled) && (
          <>
            <Grid size={{ xs: 12 }}>
              <B4Switch
                label={t("sets.tcp.general.wssizeReuse")}
                description={t("sets.tcp.general.wssizeReuseDesc")}
                message={
                  reuseMessages.length > 0 ? <>{reuseMessages}</> : undefined
                }
                info={t("sets.tcp.general.wssizeReuseInfo")}
                learnMore="sets/tcp/general#wssize"
                checked={reuse.enabled}
                path="tcp.wssize_reuse.enabled"
                onChange={(checked) =>
                  onChange("tcp.wssize_reuse.enabled", checked)
                }
                aiTopic="tcp.wssize_reuse.enabled"
              />
            </Grid>
            {reuse.enabled && (
              <Grid size={{ xs: 12, md: 6 }}>
                <B4NumberField
                  label={t("sets.tcp.general.wssizeReuseAge")}
                  helperText={t("sets.tcp.general.wssizeReuseAgeDesc")}
                  info={t("sets.tcp.general.wssizeReuseAgeInfo")}
                  value={reuse.max_age_seconds}
                  min={1}
                  max={300}
                  path="tcp.wssize_reuse.max_age_seconds"
                  onChange={(value) =>
                    onChange("tcp.wssize_reuse.max_age_seconds", value)
                  }
                  aiTopic="tcp.wssize_reuse.max_age_seconds"
                />
              </Grid>
            )}
          </>
        )}
      </Grid>

      <B4FormHeader label={t("sets.tcp.general.packetDuplication")} />
      <Grid container spacing={3} mb={3} mt={2}>
        <Grid size={{ xs: 12 }}>
          <B4Switch
            label={t("sets.tcp.general.dupEnable")}
            description={t("sets.tcp.general.dupEnableDesc")}
            message={
              dup.enabled ? (
                <B4FieldMessage>
                  {t("sets.tcp.general.dupExclusive")}
                </B4FieldMessage>
              ) : undefined
            }
            info={t("sets.tcp.general.dupInfo")}
            learnMore="sets/tcp/general"
            checked={dup.enabled}
            path="tcp.duplicate.enabled"
            onChange={(checked) => onChange("tcp.duplicate.enabled", checked)}
            aiTopic="tcp.duplicate"
            aiContext={{ count: dup.count }}
          />
        </Grid>
        {dup.enabled && (
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Slider
              label={t("sets.tcp.general.dupCount")}
              value={dup.count}
              path="tcp.duplicate.count"
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

      <B4FormHeader label={t("sets.tcp.general.ipBlockDetect")} />
      <Grid container spacing={3} mb={3} mt={2}>
        <Grid size={{ xs: 12 }}>
          <B4Switch
            label={t("sets.tcp.general.ibdEnable")}
            description={t("sets.tcp.general.ibdEnableDesc")}
            info={t(
              ibdCollects
                ? "sets.tcp.general.ibdCollectInfo"
                : "sets.tcp.general.ibdInfo",
            )}
            learnMore="sets/tcp/general"
            checked={ibd.enabled}
            path="tcp.ip_block_detect.enabled"
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
        {ibd.enabled && (
          <>
            <Grid size={{ xs: 12 }}>
              <B4Select
                label={t("sets.tcp.general.ibdAction")}
                value={ibd.action}
                path="tcp.ip_block_detect.action"
                onChange={(event) =>
                  onChange("tcp.ip_block_detect.action", event.target.value)
                }
                options={[
                  {
                    value: "block",
                    label: t("sets.tcp.general.ibdBlock"),
                    note: t("sets.tcp.general.ibdBlockNote"),
                  },
                  {
                    value: "collect",
                    label: t("sets.tcp.general.ibdCollect"),
                    note: t("sets.tcp.general.ibdCollectNote"),
                  },
                ]}
                helperText={t(
                  ibdCollects
                    ? "sets.tcp.general.ibdCollectNote"
                    : "sets.tcp.general.ibdBlockNote",
                )}
                message={
                  collectFakesClose ? (
                    <B4FieldMessage>
                      {t("sets.tcp.general.ibdCollectClosingFakes", {
                        tab: t("sets.tcp.tabs.faking"),
                        mode: t("sets.faking.desync.mode"),
                        off: t("sets.faking.desync.modeOff"),
                        ack: t("sets.faking.desync.modeAck"),
                        post: t("sets.faking.desync.postDesync"),
                      })}
                    </B4FieldMessage>
                  ) : undefined
                }
                learnMore="sets/tcp/general"
                aiTopic="tcp.ip_block_detect.action"
              />
            </Grid>

            {!ibdCollects && (
              <>
            <Grid size={{ xs: 12, md: 6 }}>
              <B4Switch
                label={t("sets.tcp.general.ibdHealDns")}
                description={t("sets.tcp.general.ibdHealDnsDesc")}
                info={t("sets.tcp.general.ibdHealDnsInfo")}
                checked={ibd.heal_dns}
                path="tcp.ip_block_detect.heal_dns"
                onChange={(checked) =>
                  onChange("tcp.ip_block_detect.heal_dns", checked)
                }
                aiTopic="tcp.ip_block_detect.heal_dns"
              />
            </Grid>

            <Grid size={{ xs: 12, md: 6 }}>
              <B4Switch
                label={t("sets.tcp.general.ibdSynDetect")}
                description={t("sets.tcp.general.ibdSynDetectDesc")}
                checked={ibd.syn_detect}
                path="tcp.ip_block_detect.syn_detect"
                onChange={(checked) =>
                  onChange("tcp.ip_block_detect.syn_detect", checked)
                }
                aiTopic="tcp.ip_block_detect.syn_detect"
              />
            </Grid>

            <Grid size={{ xs: 12, md: 6 }}>
              <B4Switch
                label={t("sets.tcp.general.ibdCache")}
                description={t("sets.tcp.general.ibdCacheDesc")}
                checked={ibd.cache_blocked_ips}
                path="tcp.ip_block_detect.cache_blocked_ips"
                onChange={(checked) =>
                  onChange("tcp.ip_block_detect.cache_blocked_ips", checked)
                }
              />
            </Grid>
              </>
            )}

            <Grid size={{ xs: 12 }}>
              <B4Advanced
                title={t("sets.tcp.general.ibdAdvanced")}
                paths={IBD_ADVANCED_PATHS}
                customized={ibdCustomized}
              >
                <Grid container spacing={3}>
                  {!ibdCollects && ibd.syn_detect && (
                    <Grid size={{ xs: 12, md: 6 }}>
                      <B4Slider
                        label={t("sets.tcp.general.ibdSynThreshold")}
                        value={ibd.syn_threshold}
                        path="tcp.ip_block_detect.syn_threshold"
                        onChange={(value: number) =>
                          onChange("tcp.ip_block_detect.syn_threshold", value)
                        }
                        min={2}
                        max={8}
                        step={1}
                        helperText={t("sets.tcp.general.ibdSynThresholdHelper")}
                        info={t("sets.tcp.general.ibdSynThresholdInfo")}
                        aiTopic="tcp.ip_block_detect.syn_threshold"
                      />
                    </Grid>
                  )}

                  {!ibdCollects && (
                  <Grid size={{ xs: 12, md: 6 }}>
                    <B4Slider
                      label={t("sets.tcp.general.ibdThreshold")}
                      value={ibd.retransmit_threshold}
                      path="tcp.ip_block_detect.retransmit_threshold"
                      onChange={(value: number) =>
                        onChange(
                          "tcp.ip_block_detect.retransmit_threshold",
                          value,
                        )
                      }
                      min={1}
                      max={10}
                      step={1}
                      helperText={t("sets.tcp.general.ibdThresholdHelper")}
                      info={t("sets.tcp.general.ibdThresholdInfo")}
                      alert={
                        ibd.retransmit_threshold <= 1 ? (
                          <B4FieldMessage>
                            {t("sets.tcp.general.ibdThresholdWarn")}
                          </B4FieldMessage>
                        ) : undefined
                      }
                      aiTopic="tcp.ip_block_detect.retransmit_threshold"
                    />
                  </Grid>
                  )}

                  <Grid size={{ xs: 12, md: 6 }}>
                    <B4Slider
                      label={t("sets.tcp.general.ibdTimeout")}
                      value={ibd.timeout_ms}
                      path="tcp.ip_block_detect.timeout_ms"
                      onChange={(value: number) =>
                        onChange("tcp.ip_block_detect.timeout_ms", value)
                      }
                      min={1000}
                      max={10000}
                      step={500}
                      valueSuffix={t("core.units.ms")}
                      helperText={t(
                        ibdCollects
                          ? "sets.tcp.general.ibdCollectTimeoutHelper"
                          : "sets.tcp.general.ibdTimeoutHelper",
                      )}
                      info={t(
                        ibdCollects
                          ? "sets.tcp.general.ibdCollectTimeoutInfo"
                          : "sets.tcp.general.ibdTimeoutInfo",
                      )}
                    />
                  </Grid>

                  {!ibdCollects && ibd.heal_dns && (
                    <Grid size={{ xs: 12, md: 6 }}>
                      <B4Slider
                        label={t("sets.tcp.general.ibdHealTtl")}
                        value={ibd.heal_ttl_sec}
                        path="tcp.ip_block_detect.heal_ttl_sec"
                        onChange={(value: number) =>
                          onChange("tcp.ip_block_detect.heal_ttl_sec", value)
                        }
                        min={10}
                        max={600}
                        step={10}
                        valueSuffix={t("core.units.s")}
                        helperText={t("sets.tcp.general.ibdHealTtlHelper")}
                        info={t("sets.tcp.general.ibdHealTtlInfo")}
                        aiTopic="tcp.ip_block_detect.heal_ttl_sec"
                      />
                    </Grid>
                  )}
                </Grid>
              </B4Advanced>
            </Grid>
          </>
        )}
      </Grid>

      <B4FormHeader label={t("sets.tcp.general.rstProtection")} />
      <Grid container spacing={3} mb={3} mt={2}>
        <Grid size={{ xs: 12 }}>
          <B4Switch
            label={t("sets.tcp.general.rstEnable")}
            description={t("sets.tcp.general.rstEnableDesc")}
            info={t("sets.tcp.general.rstInfo")}
            learnMore="sets/tcp/general"
            checked={rstProt.enabled}
            path="tcp.rst_protection.enabled"
            onChange={(checked) =>
              onChange("tcp.rst_protection.enabled", checked)
            }
            aiTopic="tcp.rst_protection"
            aiContext={{ ttl_tolerance: rstProt.ttl_tolerance }}
          />
        </Grid>

        {rstProt.enabled && (
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Slider
              label={t("sets.tcp.general.rstTtlTolerance")}
              value={rstProt.ttl_tolerance}
              path="tcp.rst_protection.ttl_tolerance"
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
        <Grid size={{ xs: 12 }}>
          <B4Switch
            label={t("sets.tcp.general.mssEnable")}
            description={t("sets.tcp.general.mssEnableDesc")}
            message={mssMessage}
            info={t("sets.tcp.general.mssInfo")}
            learnMore="sets/tcp/general#mss-clamping"
            checked={mss.enabled}
            path="mss_clamp.enabled"
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
        {mss.enabled && mssScopeOk && (
          <Grid size={{ xs: 12, md: 6 }}>
            <B4Slider
              label={t("sets.tcp.general.mssSize")}
              value={mss.size}
              path="mss_clamp.size"
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
