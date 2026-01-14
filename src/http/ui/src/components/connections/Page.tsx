import { devicesApi } from "@b4.devices";
import { SortDirection } from "@common/SortableTableCell";
import { useSnackbar } from "@context/SnackbarProvider";
import { cn } from "@design/lib/utils";
import { Card, CardContent, CardDescription } from "@design/components/ui/card";
import { Kbd, KbdGroup } from "@design/components/ui/kbd";
import {
  useDomainActions,
  useEnrichedLogs,
  useFilteredLogs,
  useParsedLogs,
  useSortedLogs,
} from "@hooks/useDomainActions";
import { useIpActions } from "@hooks/useIpActions";
import { B4Config, B4SetConfig } from "@models/config";
import {
  generateDomainVariants,
  generateIpVariants,
  loadSortState,
  saveSortState,
} from "@utils";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useWebSocket } from "../../context/B4WsProvider";
import { AddIpModal } from "./AddIpModal";
import { AddSniModal } from "./AddSniModal";
import { DomainsControlBar } from "./ControlBar";
import { DomainsTable, SortColumn } from "./Table";

const MAX_DISPLAY_ROWS = 1000;

export function ConnectionsPage() {
  const {
    domains,
    pauseDomains,
    showAll,
    setShowAll,
    setPauseDomains,
    clearDomains,
    resetDomainsBadge,
  } = useWebSocket();

  const [filter, setFilter] = useState(() => {
    return localStorage.getItem("b4_connections_filter") || "";
  });

  const [sortColumn, setSortColumn] = useState<SortColumn | null>(() => {
    const saved = loadSortState();
    return saved.column as SortColumn | null;
  });
  const [sortDirection, setSortDirection] = useState<SortDirection>(() => {
    const saved = loadSortState();
    return saved.direction;
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
  const { showSuccess } = useSnackbar();

  const [availableSets, setAvailableSets] = useState<B4SetConfig[]>([]);
  const [ipInfoToken, setIpInfoToken] = useState<string>("");
  const [devicesEnabled, setDevicesEnabled] = useState<boolean>(false);
  const [deviceMap, setDeviceMap] = useState<Record<string, string>>({});

  useEffect(() => {
    localStorage.setItem("b4_connections_filter", filter);
  }, [filter]);

  useEffect(() => {
    saveSortState(sortColumn, sortDirection);
  }, [sortColumn, sortDirection]);

  // Limit displayed rows for performance
  const recentDomains = useMemo(
    () => domains.slice(-MAX_DISPLAY_ROWS),
    [domains],
  );

  const parsedLogs = useParsedLogs(recentDomains, showAll);
  const enrichedLogs = useEnrichedLogs(parsedLogs, deviceMap);
  const filteredLogs = useFilteredLogs(enrichedLogs, filter);
  const sortedData = useSortedLogs(filteredLogs, sortColumn, sortDirection);

  useEffect(() => {
    if (!devicesEnabled) {
      setDeviceMap({});
      return;
    }
    devicesApi
      .list()
      .then((data) => {
        const map: Record<string, string> = {};
        for (const d of data.devices || []) {
          const normalized = d.mac.toUpperCase().replace(/-/g, ":");
          map[normalized] = d.alias || d.vendor || "";
        }
        setDeviceMap(map);
      })
      .catch(() => {});
  }, [devicesEnabled]);

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
        setDevicesEnabled(data.queue?.devices?.enabled || false);
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
    return () => {
      controller.abort();
    };
  }, [fetchSets]);

  const handleScrollStateChange = useCallback(() => {}, []);

  const handleSort = useCallback(
    (column: SortColumn) => {
      if (sortColumn === column) {
        // Same column clicked - cycle through: asc -> desc -> null
        if (sortDirection === "asc") {
          setSortDirection("desc");
        } else if (sortDirection === "desc") {
          // Reset sort
          setSortColumn(null);
          setSortDirection(null);
        } else {
          // null -> asc
          setSortDirection("asc");
        }
      } else {
        // Different column clicked - set to asc
        setSortColumn(column);
        setSortDirection("asc");
      }
    },
    [sortColumn, sortDirection],
  );

  const handleClearSort = useCallback(() => {
    setSortColumn(null);
    setSortDirection(null);
  }, []);

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
        showSuccess("Cleared all domains");
      } else if (e.key === "p" || e.key === "Pause") {
        e.preventDefault();
        setPauseDomains(!pauseDomains);
        showSuccess(`Domains ${pauseDomains ? "resumed" : "paused"}`);
      }
    },
    [
      clearDomains,
      pauseDomains,
      setPauseDomains,
      resetDomainsBadge,
      showSuccess,
    ],
  );

  useEffect(() => {
    globalThis.window.addEventListener("keydown", handleHotkeysDown);
    return () => {
      globalThis.window.removeEventListener("keydown", handleHotkeysDown);
    };
  }, [handleHotkeysDown]);

  return (
    <div className="flex flex-1 flex-col overflow-hidden">
      <div
        className={cn(
          "flex flex-1 flex-col overflow-hidden border transition-colors",
          pauseDomains ? "border-[rgba(158,28,96,0.5)]" : "border-border",
        )}
      >
        <DomainsControlBar
          filter={filter}
          onFilterChange={setFilter}
          totalCount={enrichedLogs.length}
          filteredCount={filteredLogs.length}
          sortColumn={sortColumn}
          paused={pauseDomains}
          showAll={showAll}
          onShowAllChange={setShowAll}
          onPauseChange={setPauseDomains}
          onClearSort={handleClearSort}
          onReset={clearDomains}
        />

        <div className="relative min-h-0 flex-1">
          <DomainsTable
            data={sortedData}
            sortColumn={sortColumn}
            sortDirection={sortDirection}
            onSort={handleSort}
            onDomainClick={handleDomainClick}
            onIpClick={handleIpClick}
            onScrollStateChange={handleScrollStateChange}
          />
        </div>
      </div>

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

      <div className="fixed right-10 bottom-10">
        <Card size="sm" variant="transparent">
          <CardContent className="space-y-2">
            <div className="flex gap-2">
              <CardDescription>Clear</CardDescription>
              <KbdGroup>
                <Kbd>Ctrl</Kbd>
                <Kbd>X</Kbd>
                <CardDescription>/</CardDescription>
                <Kbd>Del</Kbd>
              </KbdGroup>
            </div>
            <div className="flex gap-2">
              <CardDescription>Pause</CardDescription>
              <Kbd>P</Kbd>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}
