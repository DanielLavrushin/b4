import { useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { Link as RouterLink } from "react-router";
import { Box, Grid, IconButton, Link, Stack } from "@mui/material";
import { AddIcon, DiscoveryIcon, WatchdogIcon } from "@b4.icons";
import { B4Config } from "@models/config";
import { colors } from "@design";
import { dnsEndpointError } from "@utils";
import {
  B4Slider,
  B4Section,
  B4TextField,
  B4FormHeader,
  B4ChipList,
  B4FieldMessage,
  B4Help,
  B4Switch,
} from "@b4.elements";

interface CheckerSettingsProps {
  config: B4Config;
  onChange: (
    field: string,
    value: string | boolean | number | string[]
  ) => void;
}

export const CheckerSettings = ({ config, onChange }: CheckerSettingsProps) => {
  const { t } = useTranslation();
  const [newWatchdogDomain, setNewWatchdogDomain] = useState("");
  const dnsServer = config.system.checker.dns_server ?? "";
  const dnsServerError = dnsEndpointError(dnsServer);
  const watchdog = config.system.checker.watchdog;
  const setsLink = <Link component={RouterLink} to="/sets" />;
  const nothingWatched =
    (watchdog?.enabled ?? false) &&
    (watchdog?.domains ?? []).length === 0 &&
    !(config.sets ?? []).some((set) => set.discovery?.watchdog);

  const handleAddWatchdogDomain = () => {
    if (newWatchdogDomain.trim()) {
      const current = config.system.checker.watchdog?.domains || [];
      if (!current.includes(newWatchdogDomain.trim())) {
        onChange("system.checker.watchdog.domains", [
          ...current,
          newWatchdogDomain.trim(),
        ]);
      }
      setNewWatchdogDomain("");
    }
  };

  const handleRemoveWatchdogDomain = (domain: string) => {
    const current = config.system.checker.watchdog?.domains || [];
    onChange(
      "system.checker.watchdog.domains",
      current.filter((d) => d !== domain)
    );
  };

  return (
    <Stack spacing={3}>
    <B4Section
      title={t("settings.Checker.title")}
      description={t("settings.Checker.description")}
      icon={<DiscoveryIcon />}
      learnMore="settings/discovery"
    >
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Slider
            value={config.system.checker.discovery_timeout || 5}
            path="system.checker.discovery_timeout"
            onChange={(value) =>
              onChange("system.checker.discovery_timeout", value)
            }
            min={3}
            max={30}
            step={1}
            valueSuffix={t("core.units.s")}
            helperText={t("settings.Checker.discoveryTimeoutHelp")}
          />
        </Grid>
        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Slider
            value={config.system.checker.config_propagate_ms || 1500}
            path="system.checker.config_propagate_ms"
            onChange={(value) =>
              onChange("system.checker.config_propagate_ms", value)
            }
            min={500}
            max={5000}
            step={100}
            valueSuffix={t("core.units.ms")}
            helperText={t("settings.Checker.configPropagationHelp")}
            info={t("settings.Checker.configPropagationInfo")}
          />
        </Grid>

        <B4FormHeader label={t("settings.Checker.dnsConfig")} />
        <Grid size={{ xs: 12, lg: 6 }}>
          <B4TextField
            label={t("settings.Checker.trustedDns")}
            value={dnsServer}
            path="system.checker.dns_server"
            onChange={(e) =>
              onChange("system.checker.dns_server", e.target.value.trim())
            }
            placeholder={t("settings.Checker.trustedDnsPlaceholder")}
            slotProps={{ inputLabel: { shrink: true } }}
            error={dnsServerError !== null}
            helperText={
              dnsServerError
                ? t(`settings.Checker.trustedDnsError.${dnsServerError}`)
                : t("settings.Checker.trustedDnsHelp")
            }
            info={t("settings.Checker.trustedDnsInfo")}
            learnMore="settings/discovery"
          />
        </Grid>
      </Grid>
    </B4Section>

    <B4Section
      title={t("settings.Watchdog.title")}
      description={t("settings.Watchdog.description")}
      icon={<WatchdogIcon />}
      learnMore="watchdog"
    >
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Switch
            checked={config.system.checker.watchdog?.enabled ?? false}
            path="system.checker.watchdog.enabled"
            onChange={(checked) =>
              onChange("system.checker.watchdog.enabled", checked)
            }
            description={t("settings.Watchdog.enableDesc")}
            message={
              nothingWatched ? (
                <B4FieldMessage severity="info">
                  <Trans
                    i18nKey="settings.Watchdog.nothingWatched"
                    components={{ a: setsLink }}
                  />
                </B4FieldMessage>
              ) : undefined
            }
            info={
              <Trans
                i18nKey="settings.Watchdog.perSetHint"
                components={{ a: setsLink }}
              />
            }
          />
        </Grid>

        {(config.system.checker.watchdog?.enabled ?? false) && (
        <>
        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Slider
            value={config.system.checker.watchdog?.interval_sec ?? 300}
            path="system.checker.watchdog.interval_sec"
            onChange={(value) =>
              onChange("system.checker.watchdog.interval_sec", value)
            }
            min={60}
            max={1800}
            step={30}
            valueSuffix={t("core.units.s")}
            helperText={t("settings.Watchdog.intervalHelp")}
          />
        </Grid>

        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Slider
            value={config.system.checker.watchdog?.failure_interval ?? 60}
            path="system.checker.watchdog.failure_interval"
            onChange={(value) =>
              onChange("system.checker.watchdog.failure_interval", value)
            }
            min={10}
            max={300}
            step={10}
            valueSuffix={t("core.units.s")}
            helperText={t("settings.Watchdog.failureIntervalHelp")}
          />
        </Grid>

        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Slider
            value={config.system.checker.watchdog?.cooldown_sec ?? 900}
            path="system.checker.watchdog.cooldown_sec"
            onChange={(value) =>
              onChange("system.checker.watchdog.cooldown_sec", value)
            }
            min={60}
            max={3600}
            step={60}
            valueSuffix={t("core.units.s")}
            helperText={t("settings.Watchdog.cooldownHelp")}
          />
        </Grid>

        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Slider
            value={config.system.checker.watchdog?.timeout_sec ?? 10}
            path="system.checker.watchdog.timeout_sec"
            onChange={(value) =>
              onChange("system.checker.watchdog.timeout_sec", value)
            }
            min={3}
            max={30}
            step={1}
            valueSuffix={t("core.units.s")}
            helperText={t("settings.Watchdog.timeoutHelp")}
          />
        </Grid>

        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Slider
            value={config.system.checker.watchdog?.max_retries ?? 3}
            path="system.checker.watchdog.max_retries"
            onChange={(value) =>
              onChange("system.checker.watchdog.max_retries", value)
            }
            min={1}
            max={10}
            step={1}
            helperText={t("settings.Watchdog.maxRetriesHelp")}
          />
        </Grid>

        <B4FormHeader label={t("settings.Watchdog.domainsConfig")} />
        <Grid size={{ xs: 12, md: 6 }}>
          <Box sx={{ display: "flex", gap: 1, alignItems: "flex-start" }}>
            <B4TextField
              label={t("settings.Watchdog.addDomain")}
              value={newWatchdogDomain}
              onChange={(e) => setNewWatchdogDomain(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  handleAddWatchdogDomain();
                }
              }}
              placeholder="e.g., youtube.com"
              helperText={t("settings.Watchdog.addDomainHelp")}
            />
            <IconButton
              onClick={handleAddWatchdogDomain}
              sx={{
                bgcolor: colors.accent.secondary,
                color: colors.secondary,
                "&:hover": { bgcolor: colors.accent.secondaryHover },
              }}
            >
              <AddIcon />
            </IconButton>
            <B4Help
              label={t("settings.Watchdog.addDomain")}
              info={t("settings.Watchdog.domainsHint")}
              mt="6px"
            />
          </Box>
        </Grid>
        <B4ChipList
          items={config.system.checker.watchdog?.domains || []}
          getKey={(d) => d}
          getLabel={(d) => d}
          onDelete={handleRemoveWatchdogDomain}
          title={t("settings.Watchdog.activeDomains")}
          gridSize={{ xs: 12, md: 6 }}
        />
        </>
        )}
      </Grid>
    </B4Section>
    </Stack>
  );
};
