import { useEffect, useMemo } from "react";
import { Grid, Typography, Box, Chip } from "@mui/material";
import {
  B4Switch,
  B4Select,
  B4RangeSlider,
  B4FieldMessage,
  B4FormHeader,
  B4Help,
} from "@b4.elements";
import { B4SetConfig, FragmentationStrategy } from "@models/config";
import { colors } from "@design";
import { ComboSettings } from "../frags/Combo";
import { DisorderSettings } from "../frags/Disorder";
import { ExtSplitSettings } from "../frags/ExtSplit";
import { FirstByteSettings } from "../frags/FirstByte";
import { TcpIpSettings } from "../frags/TcpIp";
import { useTranslation } from "react-i18next";
import { useChangedArrayItem, useChangedFields } from "@context/ChangedFieldsContext";

const METHOD_DOCS: Record<FragmentationStrategy, string> = {
  combo: "sets/tcp/splitting#combo",
  hybrid: "sets/tcp/splitting",
  disorder: "sets/tcp/splitting#disorder",
  extsplit: "sets/tcp/splitting",
  firstbyte: "sets/tcp/splitting",
  tcp: "sets/tcp/splitting",
  ip: "sets/tcp/splitting",
  tls: "sets/tcp/splitting#tls-record-splitting",
  oob: "sets/tcp/splitting#oob-out-of-band",
  none: "sets/tcp/splitting",
};

function PoolChip({
  value,
  label,
  active,
  onClick,
}: {
  value: FragmentationStrategy;
  label: string;
  active: boolean;
  onClick: () => void;
}) {
  const changed = useChangedArrayItem(
    "fragmentation.strategy_pool",
    value,
    label,
  );
  return (
    <Chip
      label={label}
      size="small"
      onClick={onClick}
      sx={{
        bgcolor: active ? colors.accent.primary : colors.background.dark,
        color: active || changed ? colors.secondary : colors.text.secondary,
        fontWeight: active || changed ? 600 : 400,
        cursor: "pointer",
        border: `1px solid ${changed ? colors.secondary : active ? colors.primary : colors.border.default}`,
        "&:hover": {
          bgcolor: active ? colors.accent.primary : colors.background.paper,
        },
      }}
    />
  );
}
interface TcpSplittingProps {
  config: B4SetConfig;
  onChange: (
    field: string,
    value: string | boolean | number | string[],
  ) => void;
}

export const TcpSplitting = ({ config, onChange }: TcpSplittingProps) => {
  const { t } = useTranslation();
  const { registerValueLabels, scopeVersion } = useChangedFields();
  const strategy = config.fragmentation.strategy;
  const isTcpOrIp = strategy === "tcp" || strategy === "ip";
  const isOob = strategy === "oob";
  const isTls = strategy === "tls";
  const middleSni = config.fragmentation.middle_sni;

  const pool = config.fragmentation.strategy_pool ?? [];
  const hasPool = pool.length > 0;

  const togglePoolStrategy = (value: FragmentationStrategy) => {
    const current: FragmentationStrategy[] = [...pool];
    const idx = current.indexOf(value);
    if (idx >= 0) {
      current.splice(idx, 1);
    } else {
      current.push(value);
    }
    onChange("fragmentation.strategy_pool", current);
  };

  const fragmentationOptions = useMemo<
    { label: string; value: FragmentationStrategy; note: string }[]
  >(
    () => [
      {
        label: t("sets.tcp.splitting.strategyCombo"),
        value: "combo",
        note: t("sets.tcp.splitting.methodComboHelper"),
      },
      {
        label: t("sets.tcp.splitting.strategyHybrid"),
        value: "hybrid",
        note: t("sets.tcp.splitting.methodHybridHelper"),
      },
      {
        label: t("sets.tcp.splitting.strategyDisorder"),
        value: "disorder",
        note: t("sets.tcp.splitting.methodDisorderHelper"),
      },
      {
        label: t("sets.tcp.splitting.strategyExtSplit"),
        value: "extsplit",
        note: t("sets.tcp.splitting.methodExtSplitHelper"),
      },
      {
        label: t("sets.tcp.splitting.strategyFirstByte"),
        value: "firstbyte",
        note: t("sets.tcp.splitting.methodFirstByteHelper"),
      },
      {
        label: t("sets.tcp.splitting.strategyTcp"),
        value: "tcp",
        note: t("sets.tcp.splitting.methodTcpHelper"),
      },
      {
        label: t("sets.tcp.splitting.strategyIp"),
        value: "ip",
        note: t("sets.tcp.splitting.methodIpHelper"),
      },
      {
        label: t("sets.tcp.splitting.strategyTls"),
        value: "tls",
        note: t("sets.tcp.splitting.methodTlsHelper"),
      },
      {
        label: t("sets.tcp.splitting.strategyOob"),
        value: "oob",
        note: t("sets.tcp.splitting.methodOobHelper"),
      },
      {
        label: t("sets.tcp.splitting.strategyNone"),
        value: "none",
        note: t("sets.tcp.splitting.methodNoneHelper"),
      },
    ],
    [t],
  );

  const methodInfo: Record<FragmentationStrategy, string | undefined> = {
    combo: t("sets.tcp.splitting.methodComboInfo"),
    hybrid: t("sets.tcp.splitting.methodHybridInfo"),
    disorder: t("sets.tcp.splitting.methodDisorderInfo"),
    extsplit: t("sets.tcp.splitting.methodExtSplitInfo"),
    firstbyte: t("sets.tcp.splitting.methodFirstByteInfo"),
    tcp: t("sets.tcp.splitting.methodTcpInfo"),
    ip: t("sets.tcp.splitting.methodIpInfo"),
    tls: t("sets.tcp.splitting.methodTlsInfo"),
    oob: t("sets.tcp.splitting.methodOobInfo"),
    none: undefined,
  };

  const poolOptions = fragmentationOptions.filter((o) => o.value !== "none");
  useEffect(() => {
    registerValueLabels(
      "fragmentation.strategy_pool",
      new Map(fragmentationOptions.map((o) => [o.value, o.label])),
    );
  }, [registerValueLabels, fragmentationOptions, scopeVersion]);

  return (
    <Grid container spacing={3} sx={{ mt: 2 }}>
      <Grid size={{ xs: 12, md: 6 }}>
        <B4Select
          label={t("sets.tcp.splitting.method")}
          value={strategy}
          options={fragmentationOptions}
          path="fragmentation.strategy"
          onChange={(e) => onChange("fragmentation.strategy", e.target.value)}
          helperText={t(
            {
              combo: "sets.tcp.splitting.methodComboHelper",
              hybrid: "sets.tcp.splitting.methodHybridHelper",
              disorder: "sets.tcp.splitting.methodDisorderHelper",
              extsplit: "sets.tcp.splitting.methodExtSplitHelper",
              firstbyte: "sets.tcp.splitting.methodFirstByteHelper",
              tcp: "sets.tcp.splitting.methodTcpHelper",
              ip: "sets.tcp.splitting.methodIpHelper",
              tls: "sets.tcp.splitting.methodTlsHelper",
              oob: "sets.tcp.splitting.methodOobHelper",
              none: "sets.tcp.splitting.methodNoneHelper",
            }[strategy],
          )}
          info={methodInfo[strategy]}
          learnMore={METHOD_DOCS[strategy]}
          aiTopic="fragmentation.strategy"
          aiContext={{
            available: fragmentationOptions.map((o) => o.value),
            pool,
          }}
        />
      </Grid>

      <Grid size={{ xs: 12, md: 6 }}>
        <B4Switch
          checked={config.fragmentation.reverse_order}
          path="fragmentation.reverse_order"
          onChange={(checked: boolean) =>
            onChange("fragmentation.reverse_order", checked)
          }
          description={t("sets.tcp.splitting.reverseOrderDesc")}
          info={t("sets.tcp.splitting.reverseOrderInfo")}
          learnMore="sets/tcp/splitting"
          aiTopic="fragmentation.reverse_order"
        />
      </Grid>

      <Grid size={{ xs: 12 }}>
        <Box sx={{ display: "flex", alignItems: "center", gap: 0.5 }}>
          <Typography variant="body2" sx={{ fontWeight: 500 }}>
            {t("sets.tcp.splitting.strategyPool")}
          </Typography>
          <B4Help
            label={t("sets.tcp.splitting.strategyPool")}
            info={t("sets.tcp.splitting.strategyPoolInfo")}
            learnMore="sets/tcp/splitting"
            aiTopic="fragmentation.strategy_pool"
            fieldDoc={t("sets.tcp.splitting.strategyPoolDesc")}
            value={pool.join(",")}
            aiContext={{
              current_strategy: strategy,
              pool_size: pool.length,
            }}
          />
        </Box>
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ mb: 1, display: "block" }}
        >
          {t("sets.tcp.splitting.strategyPoolDesc")}
        </Typography>
        <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.75 }}>
          {poolOptions.map((opt) => (
            <PoolChip
              key={opt.value}
              value={opt.value}
              label={opt.label}
              active={pool.includes(opt.value)}
              onClick={() => togglePoolStrategy(opt.value)}
            />
          ))}
        </Box>
        {hasPool && (
          <Box sx={{ mt: 1 }}>
            <B4FieldMessage severity="info">
              {t("sets.tcp.splitting.strategyPoolActive", {
                count: pool.length,
              })}
            </B4FieldMessage>
          </Box>
        )}
      </Grid>

      {isTcpOrIp && <TcpIpSettings config={config} onChange={onChange} />}

      {strategy === "combo" && (
        <ComboSettings config={config} onChange={onChange} />
      )}

      {strategy === "disorder" && (
        <DisorderSettings config={config} onChange={onChange} />
      )}
      {strategy === "extsplit" && <ExtSplitSettings />}

      {strategy === "firstbyte" && <FirstByteSettings config={config} />}

      {isOob && (
        <>
          <B4FormHeader label={t("sets.tcp.splitting.oobHeader")} />

          <Grid size={{ xs: 12, md: 6 }}>
            <B4RangeSlider
              value={[
                config.fragmentation.oob_position || 1,
                config.fragmentation.oob_position_max ||
                  config.fragmentation.oob_position ||
                  1,
              ]}
              path="fragmentation.oob_position"
              onChange={(value: [number, number]) => {
                onChange("fragmentation.oob_position", value[0]);
                onChange("fragmentation.oob_position_max", value[1]);
              }}
              min={1}
              max={50}
              step={1}
              helperText={t("sets.tcp.splitting.oobPositionHelper")}
              info={t("sets.tcp.splitting.oobPositionInfo")}
              learnMore="sets/tcp/splitting#oob-out-of-band"
              alert={
                middleSni ? (
                  <B4FieldMessage severity="info">
                    {t("sets.tcp.splitting.oobPositionSniNote")}
                  </B4FieldMessage>
                ) : undefined
              }
              aiTopic="fragmentation.oob_position"
            />
          </Grid>

          <Grid size={{ xs: 12, md: 6 }}>
            <Box>
              <Typography variant="body2" gutterBottom>
                {t("sets.tcp.splitting.oobByte")}{" "}
                <code>
                  {String.fromCodePoint(config.fragmentation.oob_char || 120)}
                </code>{" "}
                (0x
                {(config.fragmentation.oob_char || 120)
                  .toString(16)
                  .padStart(2, "0")}
                )
              </Typography>
            </Box>
          </Grid>
        </>
      )}

      {isTls && (
        <>
          <B4FormHeader label={t("sets.tcp.splitting.tlsRecHeader")} />

          <Grid size={{ xs: 12, md: 6 }}>
            <B4RangeSlider
              label={t("sets.tcp.splitting.tlsRecPosition")}
              value={[
                config.fragmentation.tlsrec_pos || 1,
                config.fragmentation.tlsrec_pos_max ||
                  config.fragmentation.tlsrec_pos ||
                  1,
              ]}
              path="fragmentation.tlsrec_pos"
              onChange={(value: [number, number]) => {
                onChange("fragmentation.tlsrec_pos", value[0]);
                onChange("fragmentation.tlsrec_pos_max", value[1]);
              }}
              min={1}
              max={100}
              step={1}
              helperText={t("sets.tcp.splitting.tlsRecPositionHelper")}
              info={t("sets.tcp.splitting.tlsRecPositionInfo")}
              learnMore="sets/tcp/splitting#tls-record-splitting"
              alert={
                middleSni ? (
                  <B4FieldMessage severity="info">
                    {t("sets.tcp.splitting.tlsRecPositionSniNote")}
                  </B4FieldMessage>
                ) : undefined
              }
              aiTopic="fragmentation.tlsrec_pos"
            />
          </Grid>
        </>
      )}
    </Grid>
  );
};
