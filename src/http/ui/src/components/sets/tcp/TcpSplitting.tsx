import { Grid, Typography, Box, Chip } from "@mui/material";
import {
  B4Switch,
  B4Select,
  B4RangeSlider,
  B4Alert,
  B4FormHeader,
  B4Hint,
  B4AiExplain,
  aiHoverRevealSx,
} from "@b4.elements";
import { B4SetConfig, FragmentationStrategy } from "@models/config";
import { colors } from "@design";
import { ComboSettings } from "../frags/Combo";
import { DisorderSettings } from "../frags/Disorder";
import { ExtSplitSettings } from "../frags/ExtSplit";
import { FirstByteSettings } from "../frags/FirstByte";
import { TcpIpSettings } from "../frags/TcpIp";
import { useTranslation } from "react-i18next";

interface TcpSplittingProps {
  config: B4SetConfig;
  onChange: (
    field: string,
    value: string | boolean | number | string[],
  ) => void;
}

export const TcpSplitting = ({ config, onChange }: TcpSplittingProps) => {
  const { t } = useTranslation();
  const strategy = config.fragmentation.strategy;
  const isTcpOrIp = strategy === "tcp" || strategy === "ip";
  const isOob = strategy === "oob";
  const isTls = strategy === "tls";
  const isActive = strategy !== "none";

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

  const fragmentationOptions: {
    label: string;
    value: FragmentationStrategy;
  }[] = [
    { label: t("sets.tcp.splitting.strategyCombo"), value: "combo" },
    { label: t("sets.tcp.splitting.strategyHybrid"), value: "hybrid" },
    { label: t("sets.tcp.splitting.strategyDisorder"), value: "disorder" },
    { label: t("sets.tcp.splitting.strategyExtSplit"), value: "extsplit" },
    { label: t("sets.tcp.splitting.strategyFirstByte"), value: "firstbyte" },
    { label: t("sets.tcp.splitting.strategyTcp"), value: "tcp" },
    { label: t("sets.tcp.splitting.strategyIp"), value: "ip" },
    { label: t("sets.tcp.splitting.strategyTls"), value: "tls" },
    { label: t("sets.tcp.splitting.strategyOob"), value: "oob" },
    { label: t("sets.tcp.splitting.strategyNone"), value: "none" },
  ];

  const poolOptions = fragmentationOptions.filter((o) => o.value !== "none");

  return (
    <Grid container spacing={3} sx={{ mt: 2 }}>
      {/* Strategy Selection */}
      <Grid size={{ xs: 12, md: 6 }}>
        <B4Select
          label={t("sets.tcp.splitting.method")}
          value={strategy}
          options={fragmentationOptions}
          onChange={(e) => onChange("fragmentation.strategy", e.target.value)}
          aiTopic="fragmentation.strategy"
          aiContext={{
            available: fragmentationOptions.map((o) => o.value),
            pool,
          }}
        />
      </Grid>

      <Grid size={{ xs: 12, md: 6 }}>
        <B4Switch
          label={t("sets.tcp.splitting.reverseOrder")}
          checked={config.fragmentation.reverse_order}
          onChange={(checked: boolean) =>
            onChange("fragmentation.reverse_order", checked)
          }
          description={t("sets.tcp.splitting.reverseOrderDesc")}
          aiTopic="fragmentation.reverse_order"
        />
      </Grid>

      <Grid size={{ xs: 12 }}>
        <Box
          sx={{
            display: "flex",
            alignItems: "flex-start",
            gap: 1,
            ...aiHoverRevealSx,
          }}
        >
          <Box sx={{ flex: 1 }}>
            <Typography variant="body2" sx={{ fontWeight: 500, mb: 1 }}>
              {t("sets.tcp.splitting.strategyPool")}
            </Typography>
            <Typography
              variant="caption"
              sx={{
                color: "text.secondary",
                mb: 1,
                display: "block"
              }}>
              {t("sets.tcp.splitting.strategyPoolDesc")}
            </Typography>
          </Box>
          <B4AiExplain
            topic="fragmentation.strategy_pool"
            fieldLabel={t("sets.tcp.splitting.strategyPool")}
            fieldDoc={t("sets.tcp.splitting.strategyPoolDesc")}
            value={pool.join(",")}
            context={{
              current_strategy: strategy,
              pool_size: pool.length,
            }}
          />
        </Box>
        <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.75 }}>
          {poolOptions.map((opt) => {
            const active: boolean = pool.includes(opt.value);
            return (
              <Chip
                key={opt.value}
                label={opt.label}
                size="small"
                onClick={() => togglePoolStrategy(opt.value)}
                sx={{
                  bgcolor: active
                    ? colors.accent.primary
                    : colors.background.dark,
                  color: active ? colors.secondary : colors.text.secondary,
                  fontWeight: active ? 600 : 400,
                  cursor: "pointer",
                  border: active
                    ? `1px solid ${colors.secondary}`
                    : `1px solid ${colors.border.default}`,
                  "&:hover": {
                    bgcolor: active
                      ? colors.accent.primary
                      : colors.background.paper,
                  },
                }}
              />
            );
          })}
        </Box>
        {hasPool && (
          <B4Hint sx={{ mt: 1 }}>
            {t("sets.tcp.splitting.strategyPoolActive", {
              count: pool.length,
            })}
          </B4Hint>
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

          <B4Hint>{t("sets.tcp.splitting.oobAlert")}</B4Hint>

          <Grid size={{ xs: 12, md: 6 }}>
            <B4RangeSlider
              label={t("sets.tcp.splitting.oobPosition")}
              value={[
                config.fragmentation.oob_position || 1,
                config.fragmentation.oob_position_max ||
                  config.fragmentation.oob_position ||
                  1,
              ]}
              onChange={(value: [number, number]) => {
                onChange("fragmentation.oob_position", value[0]);
                onChange("fragmentation.oob_position_max", value[1]);
              }}
              min={1}
              max={50}
              step={1}
              helperText={t("sets.tcp.splitting.oobPositionHelper")}
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

      {/* TLS Record Settings */}
      {isTls && (
        <>
          <B4FormHeader label={t("sets.tcp.splitting.tlsRecHeader")} />

          <B4Hint>{t("sets.tcp.splitting.tlsRecAlert")}</B4Hint>

          <Grid size={{ xs: 12, md: 6 }}>
            <B4RangeSlider
              label={t("sets.tcp.splitting.tlsRecPosition")}
              value={[
                config.fragmentation.tlsrec_pos || 1,
                config.fragmentation.tlsrec_pos_max ||
                  config.fragmentation.tlsrec_pos ||
                  1,
              ]}
              onChange={(value: [number, number]) => {
                onChange("fragmentation.tlsrec_pos", value[0]);
                onChange("fragmentation.tlsrec_pos_max", value[1]);
              }}
              min={1}
              max={100}
              step={1}
              helperText={t("sets.tcp.splitting.tlsRecPositionHelper")}
              aiTopic="fragmentation.tlsrec_pos"
            />
          </Grid>
        </>
      )}

      {!isActive && (
        <B4Alert severity="warning">
          {t("sets.tcp.splitting.disabledWarning")}
        </B4Alert>
      )}
    </Grid>
  );
};
