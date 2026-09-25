import { useState } from "react";
import { Trans, useTranslation } from "react-i18next";
import { Link as RouterLink } from "react-router";
import { Box, Grid, IconButton, Link, Stack } from "@mui/material";
import { AddIcon, DiscoveryIcon, WatchdogIcon } from "@b4.icons";
import { B4Config } from "@models/config";
import { colors } from "@design";
import {
  B4Slider,
  B4Section,
  B4TextField,
  B4FormHeader,
  B4ChipList,
  B4Hint,
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
  const [newDns, setNewDns] = useState("");
  const [newWatchdogDomain, setNewWatchdogDomain] = useState("");

  const handleAddDns = () => {
    if (newDns.trim()) {
      const current = config.system.checker.reference_dns || [];
      if (!current.includes(newDns.trim())) {
        onChange("system.checker.reference_dns", [...current, newDns.trim()]);
      }
      setNewDns("");
    }
  };

  const handleRemoveDns = (dns: string) => {
    const current = config.system.checker.reference_dns || [];
    onChange(
      "system.checker.reference_dns",
      current.filter((s) => s !== dns)
    );
  };

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
    >
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Slider
            label={t("settings.Checker.discoveryTimeout")}
            value={config.system.checker.discovery_timeout || 5}
            onChange={(value) =>
              onChange("system.checker.discovery_timeout", value)
            }
            min={3}
            max={30}
            step={1}
            valueSuffix=" sec"
            helperText={t("settings.Checker.discoveryTimeoutHelp")}
          />
        </Grid>
        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Slider
            label={t("settings.Checker.configPropagation")}
            value={config.system.checker.config_propagate_ms || 1500}
            onChange={(value) =>
              onChange("system.checker.config_propagate_ms", value)
            }
            min={500}
            max={5000}
            step={100}
            valueSuffix=" ms"
            helperText={t("settings.Checker.configPropagationHelp")}
          />
        </Grid>
        <Grid size={{ xs: 12, lg: 6 }}>
          <B4TextField
            label={t("settings.Checker.referenceDomain")}
            value={config.system.checker.reference_domain || "yandex.ru"}
            onChange={(e) =>
              onChange("system.checker.reference_domain", e.target.value)
            }
            placeholder="yandex.ru"
            helperText={t("settings.Checker.referenceDomainHelp")}
          />
        </Grid>

        <B4FormHeader label={t("settings.Checker.dnsConfig")} />
        <Grid size={{ xs: 12, md: 6 }}>
          <Box sx={{ display: "flex", gap: 1, alignItems: "flex-start" }}>
            <B4TextField
              label={t("settings.Checker.addDns")}
              value={newDns}
              onChange={(e) => setNewDns(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault();
                  handleAddDns();
                }
              }}
              placeholder="e.g., 8.8.8.8"
              helperText={t("settings.Checker.addDnsHelp")}
            />
            <IconButton
              onClick={handleAddDns}
              sx={{
                bgcolor: colors.accent.secondary,
                color: colors.secondary,
                "&:hover": { bgcolor: colors.accent.secondaryHover },
              }}
            >
              <AddIcon />
            </IconButton>
          </Box>
        </Grid>
        <B4ChipList
          items={config.system.checker.reference_dns || []}
          getKey={(d) => d}
          getLabel={(d) => d}
          onDelete={handleRemoveDns}
          title={t("settings.Checker.activeDns")}
          gridSize={{ xs: 12, md: 6 }}
        />
      </Grid>
    </B4Section>

    <B4Section
      title={t("settings.Watchdog.title")}
      description={t("settings.Watchdog.description")}
      icon={<WatchdogIcon />}
    >
      <Grid container spacing={2}>
        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Switch
            label={t("settings.Watchdog.enabled")}
            checked={config.system.checker.watchdog?.enabled ?? false}
            onChange={(checked) =>
              onChange("system.checker.watchdog.enabled", checked)
            }
          />
        </Grid>

        <B4Hint>
          <Trans
            i18nKey="settings.Watchdog.perSetHint"
            components={{ a: <Link component={RouterLink} to="/sets" /> }}
          />
        </B4Hint>

        {(config.system.checker.watchdog?.enabled ?? false) && (
        <>
        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Slider
            label={t("settings.Watchdog.interval")}
            value={config.system.checker.watchdog?.interval_sec ?? 300}
            onChange={(value) =>
              onChange("system.checker.watchdog.interval_sec", value)
            }
            min={60}
            max={1800}
            step={30}
            valueSuffix=" sec"
            helperText={t("settings.Watchdog.intervalHelp")}
          />
        </Grid>

        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Slider
            label={t("settings.Watchdog.failureInterval")}
            value={config.system.checker.watchdog?.failure_interval ?? 60}
            onChange={(value) =>
              onChange("system.checker.watchdog.failure_interval", value)
            }
            min={10}
            max={300}
            step={10}
            valueSuffix=" sec"
            helperText={t("settings.Watchdog.failureIntervalHelp")}
          />
        </Grid>

        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Slider
            label={t("settings.Watchdog.cooldown")}
            value={config.system.checker.watchdog?.cooldown_sec ?? 900}
            onChange={(value) =>
              onChange("system.checker.watchdog.cooldown_sec", value)
            }
            min={60}
            max={3600}
            step={60}
            valueSuffix=" sec"
            helperText={t("settings.Watchdog.cooldownHelp")}
          />
        </Grid>

        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Slider
            label={t("settings.Watchdog.timeout")}
            value={config.system.checker.watchdog?.timeout_sec ?? 10}
            onChange={(value) =>
              onChange("system.checker.watchdog.timeout_sec", value)
            }
            min={3}
            max={30}
            step={1}
            valueSuffix=" sec"
            helperText={t("settings.Watchdog.timeoutHelp")}
          />
        </Grid>

        <Grid size={{ xs: 12, lg: 6 }}>
          <B4Slider
            label={t("settings.Watchdog.maxRetries")}
            value={config.system.checker.watchdog?.max_retries ?? 3}
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
        <B4Hint>{t("settings.Watchdog.domainsHint")}</B4Hint>
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
