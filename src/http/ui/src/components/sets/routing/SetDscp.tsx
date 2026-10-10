import { Box, Grid, Link, Typography } from "@mui/material";
import { Link as RouterLink } from "react-router";
import { Trans, useTranslation } from "react-i18next";
import {
  B4Alert,
  B4Badge,
  B4FormHeader,
  B4Hint,
  B4Slider,
  B4Switch,
} from "@b4.elements";
import { B4SetConfig } from "@models/config";
import {
  DSCP_MAX_VALUE,
  DscpEnvironment,
  dscpRefusal,
  dscpRefusalKey,
  suggestDscpValue,
} from "@utils";
import { resolveRoutingMode } from "../facets";

const FIREWALL_SETTINGS = "/settings/general/firewall";

interface SetDscpProps {
  config: B4SetConfig;
  env: DscpEnvironment;
  availableIfaces: string[];
  encapsulatedIfaces: string[];
  onChange: (field: string, value: number | boolean) => void;
}

export const SetDscp = ({
  config,
  env,
  availableIfaces,
  encapsulatedIfaces,
  onChange,
}: SetDscpProps) => {
  const { t } = useTranslation();
  const dscp = config.dscp ?? { enabled: false, value: 0 };
  const refusal = dscpRefusal(config, env.deviceFilter);
  const blocked = env.skipSetup || refusal !== null;
  const applies = dscp.enabled && !blocked;
  const global = env.global;
  const globalOn = !!global?.enabled;
  const routing = config.routing;
  const targets = config.targets;
  const egress =
    routing.enabled && resolveRoutingMode(routing.mode) === "interface"
      ? routing.egress_interface
      : "";
  const encapsulated = !!egress && encapsulatedIfaces.includes(egress);
  const learnsDomains =
    !targets.domain_only &&
    ((targets.sni_domains?.length ?? 0) > 0 ||
      (targets.geosite_categories?.length ?? 0) > 0);
  const scope = [
    ...new Set(
      (global?.interfaces ?? []).map((i) => i.trim()).filter(Boolean),
    ),
  ];
  const firewallLink = <Link component={RouterLink} to={FIREWALL_SETTINGS} />;

  const handleToggle = (checked: boolean) => {
    onChange("dscp.enabled", checked);
    if (checked && !dscp.value) {
      onChange(
        "dscp.value",
        suggestDscpValue(env.otherValues, global?.value ? [global.value] : []),
      );
    }
  };

  return (
    <>
      <B4FormHeader label={t("sets.routing.dscpTitle")} />
      <Grid size={{ xs: 12 }}>
        <B4Switch
          label={t("sets.routing.dscpEnable")}
          description={t("sets.routing.dscpEnableDesc")}
          checked={dscp.enabled}
          onChange={handleToggle}
          disabled={blocked && !dscp.enabled}
          aiTopic="dscp.enabled"
          aiContext={{ value: dscp.value }}
        />
      </Grid>

      {env.skipSetup && (
        <B4Alert severity={dscp.enabled ? "warning" : "info"}>
          {t("sets.routing.dscpOffSkip")}
        </B4Alert>
      )}
      {refusal && (
        <B4Alert severity={dscp.enabled ? "warning" : "info"}>
          {t(dscpRefusalKey(config, refusal))}
        </B4Alert>
      )}

      {dscp.enabled && (
        <Grid size={{ xs: 12, md: 6 }}>
          <B4Slider
            label={t("settings.Feature.dscpValue")}
            value={dscp.value}
            onChange={(value: number) => onChange("dscp.value", value)}
            min={0}
            max={DSCP_MAX_VALUE}
            step={1}
            helperText={t("sets.routing.dscpValueHelp")}
            aiTopic="dscp.value"
          />
        </Grid>
      )}

      {applies && globalOn && global?.value === dscp.value && (
        <B4Alert severity="warning">
          {t("sets.routing.dscpSameAsGlobal", { value: dscp.value })}
        </B4Alert>
      )}
      {applies && globalOn && global?.value !== dscp.value && (
        <B4Alert severity="info">
          {t("sets.routing.dscpReplacesGlobal", {
            value: dscp.value,
            global: global?.value,
          })}
        </B4Alert>
      )}
      {applies && !globalOn && (
        <B4Alert severity="info">
          <Trans
            i18nKey="sets.routing.dscpOthersKeep"
            values={{ value: dscp.value }}
            components={{ a: firewallLink }}
          />
        </B4Alert>
      )}
      {applies && encapsulated && (
        <B4Alert severity="warning">
          {t("sets.routing.dscpEncapsulated", { iface: egress })}
        </B4Alert>
      )}
      {applies && learnsDomains && (
        <B4Hint>{t("sets.routing.dscpFirstConnection")}</B4Hint>
      )}
      {applies && scope.length > 0 && (
        <Grid size={{ xs: 12 }}>
          <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
            <Trans
              i18nKey="sets.routing.dscpInterfaces"
              components={{ a: firewallLink }}
            />
          </Typography>
          <Box sx={{ display: "flex", flexWrap: "wrap", gap: 0.5 }}>
            {scope.map((iface) =>
              availableIfaces.includes(iface) ? (
                <B4Badge
                  key={iface}
                  label={iface}
                  variant="filled"
                  color="primary"
                />
              ) : (
                <B4Badge
                  key={iface}
                  label={`${iface} (${t("settings.Feature.missingIface")})`}
                  variant="filled"
                  color="error"
                />
              ),
            )}
          </Box>
        </Grid>
      )}
    </>
  );
};
