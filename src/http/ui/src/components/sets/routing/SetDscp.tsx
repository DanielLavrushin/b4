import { Box, Grid, Link, Typography } from "@mui/material";
import { Link as RouterLink } from "react-router";
import { Trans, useTranslation } from "react-i18next";
import {
  B4Badge,
  B4FieldMessage,
  B4FormHeader,
  B4Slider,
  B4Switch,
  IfaceDetails,
  useIfaceLabel,
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
const firewallLink = <Link component={RouterLink} to={FIREWALL_SETTINGS} />;

interface SetDscpProps {
  config: B4SetConfig;
  env: DscpEnvironment;
  availableIfaces: string[];
  ifaceDetails?: IfaceDetails;
  encapsulatedIfaces: string[];
  onChange: (field: string, value: number | boolean) => void;
}

export const SetDscp = ({
  config,
  env,
  availableIfaces,
  ifaceDetails,
  encapsulatedIfaces,
  onChange,
}: SetDscpProps) => {
  const { t } = useTranslation();
  const ifaceLabel = useIfaceLabel(ifaceDetails);
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

  const handleToggle = (checked: boolean) => {
    onChange("dscp.enabled", checked);
    if (checked && !dscp.value) {
      onChange(
        "dscp.value",
        suggestDscpValue(env.otherValues, global?.value ? [global.value] : []),
      );
    }
  };

  const offSeverity = dscp.enabled ? "warning" : "info";
  const switchMessages = [
    env.skipSetup && (
      <B4FieldMessage key="skip" severity={offSeverity}>
        {t("sets.routing.dscpOffSkip")}
      </B4FieldMessage>
    ),
    refusal && (
      <B4FieldMessage key="refusal" severity={offSeverity}>
        {t(dscpRefusalKey(config, refusal))}
      </B4FieldMessage>
    ),
    applies && learnsDomains && (
      <B4FieldMessage key="first-connection" severity="info">
        {t("sets.routing.dscpFirstConnection")}
      </B4FieldMessage>
    ),
  ].filter(Boolean);

  const valueMessages = [
    applies && globalOn && global?.value === dscp.value && (
      <B4FieldMessage key="same">
        {t("sets.routing.dscpSameAsGlobal", { value: dscp.value })}
      </B4FieldMessage>
    ),
    applies && globalOn && global?.value !== dscp.value && (
      <B4FieldMessage key="replaces" severity="info">
        {t("sets.routing.dscpReplacesGlobal", {
          value: dscp.value,
          global: global?.value,
        })}
      </B4FieldMessage>
    ),
    applies && !globalOn && (
      <B4FieldMessage key="others" severity="info">
        <Trans
          i18nKey="sets.routing.dscpOthersKeep"
          values={{ value: dscp.value }}
          components={{ a: firewallLink }}
        />
      </B4FieldMessage>
    ),
    applies && encapsulated && (
      <B4FieldMessage key="encapsulated">
        {t("sets.routing.dscpEncapsulated", { iface: egress })}
      </B4FieldMessage>
    ),
  ].filter(Boolean);

  return (
    <>
      <B4FormHeader label={t("sets.routing.dscpTitle")} />
      <Grid size={{ xs: 12 }}>
        <B4Switch
          label={t("sets.routing.dscpEnable")}
          description={t("sets.routing.dscpEnableDesc")}
          message={switchMessages.length > 0 ? switchMessages : undefined}
          info={t("sets.routing.dscpEnableInfo")}
          learnMore="sets/routing#dscp"
          checked={dscp.enabled}
          path="dscp.enabled"
          onChange={handleToggle}
          disabled={blocked && !dscp.enabled}
          aiTopic="dscp.enabled"
          aiContext={{ value: dscp.value }}
        />
      </Grid>

      {dscp.enabled && (
        <Grid size={{ xs: 12, md: 6 }}>
          <B4Slider
            label={t("sets.routing.dscpValue")}
            value={dscp.value}
            path="dscp.value"
            onChange={(value: number) => onChange("dscp.value", value)}
            min={0}
            max={DSCP_MAX_VALUE}
            step={1}
            helperText={t("sets.routing.dscpValueHelp")}
            info={t("sets.routing.dscpValueInfo")}
            learnMore="sets/routing#dscp-values"
            aiTopic="dscp.value"
            alert={valueMessages.length > 0 ? valueMessages : undefined}
          />
        </Grid>
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
                  label={ifaceLabel(iface)}
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
