import { useState, useEffect, useCallback } from "react";
import {
  Box,
  Container,
  Paper,
  ToggleButton,
  ToggleButtonGroup,
} from "@mui/material";
import { DashboardIcon, LogsIcon } from "@b4.icons";
import { AddSniModal } from "./AddSniModal";
import { AggregatedView } from "./views/AggregatedView";
import { RawView } from "./views/RawView";
import { useDomainActions, clearAsnLookupCache } from "@hooks/useDomainActions";
import { useIpActions } from "@hooks/useIpActions";
import {
  generateDomainVariants,
  generateIpVariants,
  asnStorage,
  stripPort,
} from "@utils";
import { colors } from "@design";
import { useWebSocket } from "@context/B4WsProvider";
import { AddIpModal } from "./AddIpModal";
import { B4Config, B4SetConfig } from "@models/config";
import { useSnackbar } from "@context/SnackbarProvider";
import { devicesApi } from "@b4.devices";
import { useTranslation } from "react-i18next";
import i18n from "@/i18n";

interface RipeNetworkInfo {
  asns: string[];
  prefix: string;
}

async function resolveAsn(
  ip: string,
  token: string,
): Promise<{ id: string; name: string } | null> {
  if (token) {
    const response = await fetch(
      `/api/integration/ipinfo?ip=${encodeURIComponent(ip)}`,
    );
    if (response.ok) {
      const data = (await response.json()) as { org?: string };
      const match = data.org ? /AS(\d+)/.exec(data.org) : null;
      if (match) return { id: match[1], name: data.org! };
    }
  }

  const response = await fetch(
    `/api/integration/ripestat?ip=${encodeURIComponent(ip)}`,
  );
  if (!response.ok) return null;
  const data = (await response.json()) as { data: RipeNetworkInfo };
  const asnId = data.data?.asns?.[0];
  return asnId ? { id: asnId, name: `AS${asnId}` } : null;
}

async function fetchAsnPrefixes(asnId: string): Promise<string[] | null> {
  const response = await fetch(
    `/api/integration/ripestat/asn?asn=${encodeURIComponent(asnId)}`,
  );
  if (!response.ok) return null;
  const data = (await response.json()) as {
    data: { prefixes: Array<{ prefix: string }> };
  };
  return data.data.prefixes.map((p) => p.prefix);
}

export function ConnectionsPage() {
  const { t } = useTranslation();
  const {
    domains,
    parsedDomains,
    pauseDomains,
    showAll,
    setShowAll,
    setPauseDomains,
    clearDomains,
    resetDomainsBadge,
  } = useWebSocket();

  const [view, setView] = useState<"aggregated" | "raw">(() => {
    const saved = localStorage.getItem("b4_connections_view");
    return saved === "raw" ? "raw" : "aggregated";
  });

  const [filter, setFilter] = useState(() => {
    return localStorage.getItem("b4_connections_filter") || "";
  });

  const { modalState, openModal, closeModal, selectVariant, addDomain } =
    useDomainActions();

  const {
    modalState: modalIpState,
    openModal: openIpModal,
    closeModal: closeIpModal,
    selectVariant: selectIpVariant,
    addIp,
  } = useIpActions();
  const { showSuccess, showError } = useSnackbar();

  const [availableSets, setAvailableSets] = useState<B4SetConfig[]>([]);
  const [ipInfoToken, setIpInfoToken] = useState<string>("");
  const [devicesEnabled, setDevicesEnabled] = useState<boolean>(false);
  const [deviceMap, setDeviceMap] = useState<Record<string, string>>({});
  const [ipToMac, setIpToMac] = useState<Record<string, string>>({});
  const [configIpToMac, setConfigIpToMac] = useState<Record<string, string>>(
    {},
  );
  const [configDeviceNames, setConfigDeviceNames] = useState<
    Record<string, string>
  >({});
  const [enrichingIps, setEnrichingIps] = useState<Set<string>>(new Set());
  const [asnVersion, setAsnVersion] = useState(0);

  useEffect(() => {
    localStorage.setItem("b4_connections_filter", filter);
  }, [filter]);

  useEffect(() => {
    localStorage.setItem("b4_connections_view", view);
  }, [view]);

  useEffect(() => {
    if (!devicesEnabled) {
      setDeviceMap({ ...configDeviceNames });
      setIpToMac({ ...configIpToMac });
      return;
    }
    devicesApi
      .list()
      .then((data) => {
        const map: Record<string, string> = {};
        const ipMap: Record<string, string> = {};
        for (const d of data.devices || []) {
          const normalized = d.mac.toUpperCase().replaceAll("-", ":");
          map[normalized] = d.alias || d.vendor || "";
          if (d.ip) ipMap[d.ip] = normalized;
        }
        for (const [mac, name] of Object.entries(configDeviceNames)) {
          map[mac] = name;
        }
        for (const [ip, mac] of Object.entries(configIpToMac)) {
          ipMap[ip] = mac;
        }
        setDeviceMap(map);
        setIpToMac(ipMap);
      })
      .catch(() => {});
  }, [devicesEnabled, configDeviceNames, configIpToMac]);

  const fetchSets = useCallback(async (signal?: AbortSignal) => {
    try {
      const response = await fetch("/api/config", { signal });
      if (response.ok) {
        const data = (await response.json()) as B4Config;
        if (data.sets && Array.isArray(data.sets)) {
          setAvailableSets(data.sets);
        }
        if (data.system?.api?.ipinfo_token) {
          setIpInfoToken(data.system.api.ipinfo_token);
        }
        setDevicesEnabled(
          data.queue?.devices?.enabled ||
            data.queue?.devices?.vendor_lookup ||
            false,
        );
        const names: Record<string, string> = {};
        const configIps: Record<string, string> = {};
        for (const d of data.queue?.devices?.devices || []) {
          const normalized = d.mac?.toUpperCase().replaceAll("-", ":");
          if (normalized && d.name) {
            names[normalized] = d.name;
          }
          if (normalized && d.ip) {
            configIps[d.ip] = normalized;
          }
        }
        setConfigDeviceNames(names);
        setConfigIpToMac(configIps);
      }
    } catch (error) {
      if ((error as Error).name !== "AbortError") {
        console.error("Failed to fetch sets:", error);
      }
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void fetchSets(controller.signal);
    void asnStorage.init();
    return () => {
      controller.abort();
    };
  }, [fetchSets]);

  const handleEnrichIp = useCallback(
    async (ip: string) => {
      const cleanIp = stripPort(ip);
      setEnrichingIps((prev) => new Set(prev).add(cleanIp));
      try {
        const asn = await resolveAsn(cleanIp, ipInfoToken);
        if (!asn) {
          showError(t("connections.table.enrichNoAsn"));
          return;
        }
        const prefixes = await fetchAsnPrefixes(asn.id);
        if (!prefixes) {
          showError(t("connections.table.enrichFailed"));
          return;
        }
        await asnStorage.addAsn(asn.id, asn.name, prefixes);
        clearAsnLookupCache();
        setAsnVersion((v) => v + 1);
        showSuccess(
          t("connections.table.enrichSuccess", {
            asn: asn.name,
            count: prefixes.length,
          }),
        );
      } catch {
        showError(t("connections.table.enrichFailed"));
      } finally {
        setEnrichingIps((prev) => {
          const next = new Set(prev);
          next.delete(cleanIp);
          return next;
        });
      }
    },
    [ipInfoToken, showSuccess, showError, t],
  );

  const handleDeleteAsn = useCallback(
    (asnId: string) => {
      void (async () => {
        await asnStorage.deleteAsn(asnId);
        clearAsnLookupCache();
        setAsnVersion((v) => v + 1);
        showSuccess(t("connections.table.asnDeleted", { asn: asnId }));
      })();
    },
    [showSuccess, t],
  );

  const handleIpClick = useCallback(
    (ip: string) => {
      const variants = generateIpVariants(ip);
      openIpModal(ip, variants);
    },
    [openIpModal],
  );

  const handleDomainClick = useCallback(
    (domain: string) => {
      const variants = generateDomainVariants(domain);
      openModal(domain, variants);
    },
    [openModal],
  );

  const handleHotkeysDown = useCallback(
    (e: KeyboardEvent) => {
      const target = e.target as HTMLElement;
      if (
        target.tagName === "INPUT" ||
        target.tagName === "TEXTAREA" ||
        target.isContentEditable
      ) {
        return;
      }

      if ((e.ctrlKey && e.key === "x") || e.key === "Delete") {
        e.preventDefault();
        clearDomains();
        resetDomainsBadge();
        showSuccess(i18n.t("connections.page.clearedAll"));
      } else if (e.key === "p" || e.key === "Pause") {
        e.preventDefault();
        setPauseDomains(!pauseDomains);
        showSuccess(
          pauseDomains
            ? i18n.t("connections.page.resumed")
            : i18n.t("connections.page.paused"),
        );
      }
    },
    [
      clearDomains,
      resetDomainsBadge,
      showSuccess,
      setPauseDomains,
      pauseDomains,
    ],
  );

  useEffect(() => {
    globalThis.window.addEventListener("keydown", handleHotkeysDown);
    return () => {
      globalThis.window.removeEventListener("keydown", handleHotkeysDown);
    };
  }, [handleHotkeysDown]);

  return (
    <Container
      maxWidth={false}
      sx={{
        flex: 1,
        py: 3,
        px: 3,
        display: "flex",
        flexDirection: "column",
        overflow: "hidden",
      }}
    >
      <Paper
        elevation={0}
        variant="outlined"
        sx={{
          flex: 1,
          display: "flex",
          flexDirection: "column",
          overflow: "hidden",
          border: "1px solid",
          borderColor: pauseDomains
            ? colors.border.strong
            : colors.border.default,
          transition: "border-color 0.3s",
        }}
      >
        <Box
          sx={{
            display: "flex",
            justifyContent: "flex-end",
            px: 2,
            pt: 1,
            pb: 0.5,
            borderBottom: `1px solid ${colors.border.light}`,
            bgcolor: colors.background.control,
          }}
        >
          <ToggleButtonGroup
            size="small"
            exclusive
            value={view}
            onChange={(_, v: "aggregated" | "raw" | null) => v && setView(v)}
            sx={{
              "& .MuiToggleButton-root": {
                px: 1.2,
                py: 0.2,
                color: colors.text.secondary,
                borderColor: colors.border.default,
                fontSize: 12,
              },
              "& .Mui-selected": {
                color: `${colors.secondary} !important`,
                bgcolor: `${colors.accent.secondary} !important`,
              },
            }}
          >
            <ToggleButton value="aggregated">
              <DashboardIcon sx={{ fontSize: 14, mr: 0.5 }} />
              {t("connections.aggregated.tabAggregated")}
            </ToggleButton>
            <ToggleButton value="raw">
              <LogsIcon sx={{ fontSize: 14, mr: 0.5 }} />
              {t("connections.aggregated.tabRaw")}
            </ToggleButton>
          </ToggleButtonGroup>
        </Box>

        {view === "aggregated" ? (
          <AggregatedView
            lines={domains}
            deviceMap={deviceMap}
            ipToMac={ipToMac}
            paused={pauseDomains}
            onTogglePause={() => setPauseDomains(!pauseDomains)}
            showAll={showAll}
            onShowAllChange={setShowAll}
            onReset={clearDomains}
            filter={filter}
            onFilterChange={setFilter}
            enrichingIps={enrichingIps}
            onAddDomain={handleDomainClick}
            onAddIp={handleIpClick}
            onEnrichAsn={(ip) => {
              void handleEnrichIp(ip);
            }}
            onDeleteAsn={handleDeleteAsn}
          />
        ) : (
          <RawView
            entries={parsedDomains}
            deviceMap={deviceMap}
            paused={pauseDomains}
            onTogglePause={() => setPauseDomains(!pauseDomains)}
            showAll={showAll}
            onShowAllChange={setShowAll}
            onReset={clearDomains}
            filter={filter}
            onFilterChange={setFilter}
            enrichingIps={enrichingIps}
            asnVersion={asnVersion}
            onAddDomain={handleDomainClick}
            onAddIp={handleIpClick}
            onEnrichIp={handleEnrichIp}
            onDeleteAsn={handleDeleteAsn}
          />
        )}
      </Paper>

      <AddSniModal
        open={modalState.open}
        domain={modalState.domain}
        variants={modalState.variants}
        selected={modalState.selected}
        onClose={closeModal}
        onSelectVariant={selectVariant}
        sets={availableSets}
        onAdd={(...args) => {
          void (async () => {
            await addDomain(...args);
            await fetchSets();
          })();
        }}
      />

      <AddIpModal
        open={modalIpState.open}
        ip={modalIpState.ip}
        variants={modalIpState.variants}
        selected={modalIpState.selected as string}
        sets={availableSets}
        ipInfoToken={ipInfoToken}
        onClose={closeIpModal}
        onSelectVariant={selectIpVariant}
        onAdd={(...args) => {
          void (async () => {
            await addIp(...args);
            await fetchSets();
          })();
        }}
        onAddHostname={(hostname) => {
          const variants = generateDomainVariants(hostname);
          openModal(hostname, variants);
        }}
      />
    </Container>
  );
}
