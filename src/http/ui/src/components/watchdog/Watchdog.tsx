import { useEffect, useState } from "react";
import { useTranslation, Trans } from "react-i18next";
import { Link as RouterLink } from "react-router";
import { IconButton, Link, Stack } from "@mui/material";
import { WatchdogIcon, RefreshIcon } from "@b4.icons";
import { B4Section, B4Alert, B4Badge, B4Help } from "@b4.elements";
import { setsApi } from "@api/sets";
import { B4SetConfig } from "@models/config";
import { useWatchdog } from "@hooks/useWatchdog";
import { SetsSection } from "./SetsSection";
import { DomainsSection } from "./DomainsSection";

export function WatchdogMonitor() {
  const { t } = useTranslation();
  const {
    state,
    loading,
    forceCheck,
    addDomain,
    removeDomain,
    toggleEnabled,
    checkSet,
    setSetEnabled,
    moveDomain,
    refresh,
  } = useWatchdog();
  const [setNames, setSetNames] = useState<Map<string, string>>(new Map());
  const [setConfigs, setSetConfigs] = useState<Map<string, B4SetConfig>>(
    new Map(),
  );

  useEffect(() => {
    let active = true;
    setsApi
      .getSets()
      .then((list) => {
        if (!active) return;
        const map = new Map<string, string>();
        const configs = new Map<string, B4SetConfig>();
        for (const s of Array.isArray(list) ? list : []) {
          map.set(s.id, s.name || s.id);
          configs.set(s.id, s);
        }
        setSetNames(map);
        setSetConfigs(configs);
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, []);

  const handleRefresh = () => {
    refresh().catch(() => {});
  };

  if (loading || !state) {
    return null;
  }

  const domains = state.domains ?? [];
  const sets = state.sets ?? [];

  return (
    <Stack spacing={3}>
      <B4Section
        title={t("watchdog.title")}
        description={t("watchdog.description")}
        icon={<WatchdogIcon />}
        learnMore="watchdog"
      >
        <Stack spacing={2}>
          <Stack direction="row" justifyContent="space-between" alignItems="center">
            <Stack direction="row" alignItems="center" spacing={0.5}>
              <B4Badge
                label={
                  state.enabled ? t("watchdog.enabled") : t("watchdog.disabled")
                }
                color={state.enabled ? "primary" : "default"}
                onClick={() => {
                  void toggleEnabled(!state.enabled);
                }}
                sx={{ cursor: "pointer" }}
              />
              <B4Help
                label={t("watchdog.title")}
                info={t("watchdog.alert")}
                credit={
                  <>
                    {t("watchdog.inspiredBy")}{" "}
                    <Link
                      href="https://github.com/belotserkovtsev/ladon"
                      target="_blank"
                      rel="noopener noreferrer"
                    >
                      belotserkovtsev/ladon
                    </Link>{" "}
                    {t("watchdog.project")}
                  </>
                }
              />
            </Stack>
            <IconButton onClick={handleRefresh} size="small">
              <RefreshIcon sx={{ fontSize: 20 }} />
            </IconButton>
          </Stack>

          {!state.enabled && (
            <B4Alert severity="info">
              <Trans
                i18nKey="watchdog.disabledHint"
                components={{ a: <Link component={RouterLink} to="/settings/discovery" /> }}
              />
            </B4Alert>
          )}
        </Stack>
      </B4Section>

      <SetsSection
        sets={sets}
        enabled={state.enabled}
        setNames={setNames}
        onCheck={checkSet}
        onTurnOff={(setId) => setSetEnabled(setId, false)}
      />

      <DomainsSection
        domains={domains}
        enabled={state.enabled}
        setConfigs={setConfigs}
        onForceCheck={(d) => {
          void forceCheck(d);
        }}
        onRemove={(d) => {
          void removeDomain(d);
        }}
        onAdd={addDomain}
        onMove={moveDomain}
      />
    </Stack>
  );
}
