import { useCallback, useEffect, useMemo, useState } from "react";
import {
  Box,
  Button,
  Checkbox,
  Collapse,
  Stack,
  ToggleButton,
  ToggleButtonGroup,
  Tooltip,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { useLocation } from "react-router";
import { StartIcon, ExpandIcon, CollapseIcon } from "@b4.icons";
import { colors } from "@design";
import { B4TextField, B4ChipList, B4PlusButton, B4Switch, B4Alert } from "@b4.elements";
import type { B4SetConfig } from "@models/config";
import type { DetectorLists, DetectorOptions, DetectorScope, FetchMode, IPVersion } from "@models/detector";

const URL_SEPARATORS = /\s+|,(?=\s|$)/;
const SITES_KEY = "detector_sites";
const OPTIONS_KEY = "detector_options";
const SCOPES: DetectorScope[] = ["sites", "dns", "hosting", "telegram"];

interface StoredOptions {
  scopes: DetectorScope[];
  ipVersion: IPVersion;
  parallel: number;
  fetchMode: FetchMode;
  tls12: boolean;
  sniSearch: boolean;
}

const defaultOptions: StoredOptions = {
  scopes: ["sites", "dns"],
  ipVersion: "ipv4",
  parallel: 5,
  fetchMode: "both",
  tls12: true,
  sniSearch: false,
};

function loadJSON<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    if (raw) return { ...fallback, ...JSON.parse(raw) } as T;
  } catch {
    /* ignore */
  }
  return fallback;
}

const extractDomain = (url: string): string => {
  try {
    const withProto = url.includes("://") ? url : `https://${url}`;
    const u = new URL(withProto);
    return u.pathname && u.pathname !== "/" ? `${u.hostname}${u.pathname}` : u.hostname;
  } catch {
    return url;
  }
};

interface SetupPanelProps {
  sets: B4SetConfig[];
  lists: DetectorLists | null;
  listsBusy: boolean;
  busy: boolean;
  onStart: (options: DetectorOptions) => void;
  onUpdateLists: () => void;
  onResetLists: () => void;
}

export const SetupPanel = ({ sets, lists, listsBusy, busy, onStart, onUpdateLists, onResetLists }: SetupPanelProps) => {
  const { t } = useTranslation();
  const location = useLocation();
  const [sites, setSites] = useState<string[]>(() => loadJSON<{ sites: string[] }>(SITES_KEY, { sites: [] }).sites);
  const [input, setInput] = useState("");
  const [options, setOptions] = useState<StoredOptions>(() => loadJSON(OPTIONS_KEY, defaultOptions));
  const [advanced, setAdvanced] = useState(false);

  const setDomains = useMemo(() => {
    const domains = new Set<string>();
    for (const s of sets) {
      if (!s.enabled) continue;
      for (const d of s.targets?.sni_domains ?? []) {
        if (d && !d.startsWith("*") && !d.includes("/")) domains.add(d);
      }
    }
    return [...domains].slice(0, 200);
  }, [sets]);

  useEffect(() => {
    localStorage.setItem(SITES_KEY, JSON.stringify({ sites }));
  }, [sites]);
  useEffect(() => {
    localStorage.setItem(OPTIONS_KEY, JSON.stringify(options));
  }, [options]);

  const addSites = useCallback((raw: string) => {
    const parts = raw
      .split(URL_SEPARATORS)
      .map((l) => l.replace(/^["'`]+|["'`]+$/g, "").trim())
      .filter((l) => l.length > 0);
    if (parts.length === 0) return;
    setSites((prev) => {
      const seen = new Set(prev);
      const next = [...prev];
      for (const p of parts) {
        if (!seen.has(p)) {
          seen.add(p);
          next.push(p);
        }
      }
      return next;
    });
    setInput("");
  }, []);

  useEffect(() => {
    const state = location.state as { sites?: string[] } | null;
    if (state?.sites?.length) {
      setSites([]);
      addSites(state.sites.join("\n"));
      window.history.replaceState({}, "");
    }
  }, [location.state, addSites]);

  const fillFromSets = () => {
    setSites([]);
    addSites(setDomains.join("\n"));
  };

  const toggleScope = (scope: DetectorScope) =>
    setOptions((o) => ({
      ...o,
      scopes: o.scopes.includes(scope) ? o.scopes.filter((s) => s !== scope) : [...o.scopes, scope],
    }));

  const start = () => {
    onStart({
      sites,
      scopes: SCOPES.filter((s) => options.scopes.includes(s)),
      ip_version: options.ipVersion,
      parallel: options.parallel,
      fetch_mode: options.fetchMode,
      skip_tls12: !options.tls12,
      sni_search: options.sniSearch,
    });
  };

  const needsSites = options.scopes.includes("sites");
  const canStart = options.scopes.length > 0 && !busy;
  const defaultCount = lists?.site_count ?? 0;

  const scopeTime: Record<DetectorScope, string> = {
    sites: t("detector.scopes.sites.time", { count: sites.length || defaultCount }),
    dns: t("detector.scopes.dns.time", { count: lists?.dns_servers ?? 0 }),
    hosting: t("detector.scopes.hosting.time", { count: lists?.tcp_targets ?? 0 }),
    telegram: t("detector.scopes.telegram.time"),
  };

  return (
    <Stack spacing={2.5}>
      <Stack spacing={1}>
        <Box
          sx={{
            display: "flex",
            gap: 1,
            alignItems: "flex-start",
            flexWrap: "wrap",
            "& > .MuiFormControl-root": { flex: "1 1 260px", minWidth: 0 },
          }}
        >
          <B4TextField
            label={t("detector.setup.sitesLabel")}
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if ((e.key === "Enter" || e.key === "Tab") && input.trim()) {
                e.preventDefault();
                addSites(input);
              }
            }}
            onPaste={(e) => {
              const text = e.clipboardData.getData("text");
              if (URL_SEPARATORS.test(text)) {
                e.preventDefault();
                addSites(text);
              }
            }}
            placeholder={t("detector.setup.sitesPlaceholder")}
            helperText={t("detector.setup.sitesHelper")}
            disabled={busy}
          />
          <B4PlusButton onClick={() => addSites(input)} disabled={!input.trim() || busy} />
        </Box>
        <B4ChipList
          items={sites}
          getKey={(s) => s}
          getLabel={(s) => extractDomain(s)}
          onDelete={(s) => setSites((prev) => prev.filter((x) => x !== s))}
          emptyMessage={t("detector.setup.sitesEmpty", { count: defaultCount })}
          collapsedMax={24}
        />
        <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
          <Typography variant="caption" sx={{ color: colors.text.secondary }}>
            {t("detector.setup.fillFrom")}
          </Typography>
          <Button size="small" disabled={busy || !lists} onClick={() => { setSites([]); addSites((lists?.sites ?? []).join("\n")); }}>
            {t("detector.setup.fillDefault", { count: defaultCount })}
          </Button>
          <Tooltip title={setDomains.length === 0 ? t("detector.setup.fillSetsNone") : t("detector.setup.fillSetsHint")}>
            <span>
              <Button size="small" disabled={busy || setDomains.length === 0} onClick={fillFromSets}>
                {t("detector.setup.fillSets", { count: setDomains.length })}
              </Button>
            </span>
          </Tooltip>
          <Button size="small" disabled={busy || sites.length === 0} onClick={() => setSites([])}>
            {t("detector.setup.clear")}
          </Button>
        </Stack>
      </Stack>

      <Stack spacing={1}>
        <Typography variant="caption" sx={{ color: colors.text.secondary, textTransform: "uppercase", letterSpacing: "0.05em" }}>
          {t("detector.setup.whatToCheck")}
        </Typography>
        <Box sx={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(230px, 1fr))", gap: 1 }}>
          {SCOPES.map((scope) => {
            const on = options.scopes.includes(scope);
            return (
              <Box
                key={scope}
                role="button"
                tabIndex={0}
                onClick={() => !busy && toggleScope(scope)}
                onKeyDown={(e) => {
                  if (e.key === " " || e.key === "Enter") {
                    e.preventDefault();
                    toggleScope(scope);
                  }
                }}
                sx={{
                  display: "flex",
                  gap: 0.5,
                  alignItems: "flex-start",
                  p: 1,
                  pr: 1.5,
                  borderRadius: 1,
                  cursor: busy ? "default" : "pointer",
                  border: `1px solid ${on ? colors.border.strong : colors.border.light}`,
                  bgcolor: on ? colors.accent.secondaryHover : colors.background.control,
                }}
              >
                <Checkbox size="small" checked={on} disabled={busy} sx={{ p: 0.5, mt: -0.25 }} tabIndex={-1} />
                <Box sx={{ minWidth: 0 }}>
                  <Typography variant="body2" sx={{ fontWeight: 600 }}>
                    {t(`detector.scopes.${scope}.name`)}
                  </Typography>
                  <Typography variant="caption" sx={{ color: colors.text.secondary, display: "block" }}>
                    {t(`detector.scopes.${scope}.desc`)}
                  </Typography>
                  <Typography variant="caption" sx={{ color: colors.text.disabled }}>
                    {scopeTime[scope]}
                  </Typography>
                </Box>
              </Box>
            );
          })}
        </Box>
      </Stack>

      <Box>
        <Button
          size="small"
          onClick={() => setAdvanced((a) => !a)}
          startIcon={advanced ? <CollapseIcon /> : <ExpandIcon />}
          sx={{ color: colors.secondary }}
        >
          {t("detector.setup.advanced")}
        </Button>
        <Collapse in={advanced}>
          <Box sx={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", gap: 2, mt: 1 }}>
            <OptionGroup label={t("detector.setup.ipFamily")}>
              <ToggleButtonGroup size="small" exclusive value={options.ipVersion} onChange={(_, v: IPVersion | null) => v && setOptions((o) => ({ ...o, ipVersion: v }))}>
                <ToggleButton value="ipv4">IPv4</ToggleButton>
                <ToggleButton value="ipv6">IPv6</ToggleButton>
              </ToggleButtonGroup>
            </OptionGroup>
            <OptionGroup label={t("detector.setup.parallel")}>
              <ToggleButtonGroup size="small" exclusive value={options.parallel} onChange={(_, v: number | null) => v && setOptions((o) => ({ ...o, parallel: v }))}>
                {[1, 5, 10, 20].map((n) => (
                  <ToggleButton key={n} value={n}>{n}</ToggleButton>
                ))}
              </ToggleButtonGroup>
            </OptionGroup>
            <OptionGroup label={t("detector.setup.fetchMode")}>
              <ToggleButtonGroup size="small" exclusive value={options.fetchMode} onChange={(_, v: FetchMode | null) => v && setOptions((o) => ({ ...o, fetchMode: v }))}>
                <ToggleButton value="both">{t("detector.setup.fetchBoth")}</ToggleButton>
                <ToggleButton value="direct">{t("detector.setup.fetchDirect")}</ToggleButton>
              </ToggleButtonGroup>
            </OptionGroup>
            <B4Switch label={t("detector.setup.tls12")} description={t("detector.setup.tls12Desc")} checked={options.tls12} onChange={(v) => setOptions((o) => ({ ...o, tls12: v }))} />
            <B4Switch label={t("detector.setup.sniSearch")} description={t("detector.setup.sniSearchDesc")} checked={options.sniSearch} onChange={(v) => setOptions((o) => ({ ...o, sniSearch: v }))} />
          </Box>
        </Collapse>
      </Box>

      {needsSites && sites.length === 0 && (
        <B4Alert severity="info">{t("detector.setup.defaultListNote", { count: defaultCount })}</B4Alert>
      )}

      <Box>
        <Button variant="contained" startIcon={<StartIcon />} onClick={start} disabled={!canStart}>
          {t("detector.setup.run")}
        </Button>
      </Box>

      {lists && (
        <Typography variant="caption" sx={{ color: colors.text.disabled, maxWidth: "90ch" }}>
          {t("detector.setup.listsInfo", { sites: lists.site_count, targets: lists.tcp_targets, resolvers: lists.dns_servers, date: lists.lists_date })}{" "}
          <Tooltip title={t("detector.setup.listsUpdateHint")}>
            <Box component="span" onClick={() => !listsBusy && !busy && onUpdateLists()} sx={{ color: colors.secondary, cursor: listsBusy ? "wait" : "pointer" }}>
              {listsBusy ? t("detector.setup.listsUpdating") : t("detector.setup.listsUpdate")}
            </Box>
          </Tooltip>
          {lists.custom && (
            <>
              {" · "}
              <Box component="span" onClick={onResetLists} sx={{ color: colors.secondary, cursor: "pointer" }}>
                {t("detector.setup.listsReset", { date: lists.embedded_date })}
              </Box>
            </>
          )}
        </Typography>
      )}
    </Stack>
  );
};

const OptionGroup = ({ label, children }: { label: string; children: React.ReactNode }) => (
  <Stack spacing={0.5}>
    <Typography variant="caption" sx={{ color: colors.text.secondary }}>
      {label}
    </Typography>
    <Box>{children}</Box>
  </Stack>
);
