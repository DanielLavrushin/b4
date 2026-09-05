import { useMemo, useState } from "react";
import {
  Box,
  Button,
  Checkbox,
  IconButton,
  Menu,
  MenuItem,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Tooltip,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { AddIcon, DiscoveryIcon, SetsIcon } from "@b4.icons";
import { colors } from "@design";
import { B4Badge } from "@b4.elements";
import type { B4SetConfig } from "@models/config";
import type { SiteResult, SitesResult } from "@models/detector";
import { FetchChip, StatusChip, outcomeColor } from "./statuses";

type Filter = "all" | "problems" | "still";

interface SitesTableProps {
  result: SitesResult;
  both: boolean;
  sets: B4SetConfig[];
  onDiscovery: (sites: string[]) => void;
  onOpenSet: (id: string) => void;
  onAddToSet: (setId: string, domains: string[]) => void;
}

const problem = (s: SiteResult) => ["fixed", "still_blocked", "blocked", "broken_by_b4"].includes(s.outcome);
const still = (s: SiteResult) => ["still_blocked", "blocked", "broken_by_b4"].includes(s.outcome);
const rowKey = (s: SiteResult) => `${s.input}|${s.family ?? ""}`;

const checkboxSx = {
  color: colors.text.secondary,
  "&.Mui-checked, &.MuiCheckbox-indeterminate": { color: colors.secondary },
  p: 0.5,
};

function describe(s: SiteResult, t: (k: string, o?: Record<string, unknown>) => string): string {
  const parts: string[] = [];
  const d = s.direct;
  if (!d || d.status === "CHECKING" || d.status === "PENDING") return t("detector.sites.checking");
  if (d.detail) parts.push(d.detail.replace(/; the address DoH returns \([^)]*\) loads/, ""));
  if (d.tls12 && d.tls12 !== "OK") parts.push(t("detector.sites.tls12Also", { status: t(`detector.status.${d.tls12}`, { defaultValue: d.tls12 }) }));
  else if (d.tls12 === "OK") parts.push(t("detector.sites.tls12Works"));
  if (d.http && d.http !== "OK") parts.push(t("detector.sites.httpAlso", { status: t(`detector.status.${d.http}`, { defaultValue: d.http }) }));
  const b = s.through_b4;
  if (b && b.status !== "CHECKING") {
    if (b.status === "OK" && s.outcome === "fixed") {
      parts.push(s.set_name ? t("detector.sites.fixedBySet", { set: s.set_name, ms: b.latency_ms ?? 0 }) : t("detector.sites.fixedNoSet", { ms: b.latency_ms ?? 0 }));
    } else if (b.status !== "OK") {
      parts.push(t("detector.sites.throughB4", { detail: b.detail ?? t(`detector.status.${b.status}`, { defaultValue: b.status }) }));
    }
  }
  if (s.alt_works && s.honest_ip) parts.push(t("detector.sites.altWorks", { ip: s.honest_ip }));
  if (still(s)) {
    if (!s.set_name) parts.push(t("detector.sites.noSet"));
    else if (!s.set_enabled) parts.push(t("detector.sites.setDisabled", { set: s.set_name }));
    else if (s.outcome !== "broken_by_b4") parts.push(t("detector.sites.setNotHelping", { set: s.set_name }));
  }
  return parts.join(". ");
}

export const SitesTable = ({ result, both, sets, onDiscovery, onOpenSet, onAddToSet }: SitesTableProps) => {
  const { t } = useTranslation();
  const [filter, setFilter] = useState<Filter>("all");
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [menu, setMenu] = useState<{ anchor: HTMLElement; domains: string[] } | null>(null);

  const all = useMemo(() => result.sites ?? [], [result.sites]);
  const counts = useMemo(
    () => ({ all: all.length, problems: all.filter(problem).length, still: all.filter(still).length }),
    [all],
  );
  const rows = all.filter((s) => (filter === "all" ? true : filter === "problems" ? problem(s) : still(s)));
  const selectable = rows.filter((s) => s.done && still(s));
  const selectedRows = all.filter((s) => selected.has(rowKey(s)));
  const selectedInputs = [...new Set(selectedRows.map((s) => s.input))];
  const selectedDomains = [...new Set(selectedRows.map((s) => s.domain))];
  const allSelected = selectable.length > 0 && selectable.every((s) => selected.has(rowKey(s)));
  const someSelected = selectable.some((s) => selected.has(rowKey(s)));

  const toggle = (s: SiteResult) =>
    setSelected((prev) => {
      const next = new Set(prev);
      const k = rowKey(s);
      if (next.has(k)) next.delete(k);
      else next.add(k);
      return next;
    });
  const toggleAll = () =>
    setSelected((prev) => {
      const next = new Set(prev);
      if (allSelected) selectable.forEach((s) => next.delete(rowKey(s)));
      else selectable.forEach((s) => next.add(rowKey(s)));
      return next;
    });

  const iconSx = { color: colors.text.secondary };

  return (
    <Stack spacing={1.5}>
      <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
        {(["all", "problems", "still"] as Filter[]).map((f) => (
          <Button
            key={f}
            size="small"
            variant={filter === f ? "contained" : "outlined"}
            color={filter === f ? "secondary" : "inherit"}
            onClick={() => setFilter(f)}
            sx={{ borderRadius: 999, textTransform: "none" }}
          >
            {t(`detector.sites.filter.${f}`, { count: counts[f] })}
          </Button>
        ))}
        {result.stub_ips && result.stub_ips.length > 0 && selectedInputs.length === 0 && (
          <Typography variant="caption" sx={{ color: colors.text.secondary, ml: "auto !important" }}>
            {t("detector.sites.stubs", { ips: result.stub_ips.join(", ") })}
          </Typography>
        )}
        {selectedInputs.length > 0 && (
          <Stack direction="row" spacing={1} alignItems="center" sx={{ ml: "auto !important" }}>
            <Typography variant="caption" sx={{ color: colors.text.secondary }}>
              {t("detector.sites.selected", { count: selectedInputs.length })}
            </Typography>
            <Button
              size="small"
              variant="contained"
              startIcon={<DiscoveryIcon />}
              onClick={() => onDiscovery(selectedInputs)}
              sx={{ bgcolor: colors.secondary, color: colors.background.default, "&:hover": { bgcolor: colors.primary }, whiteSpace: "nowrap" }}
            >
              {t("detector.sites.fixSelected", { count: selectedInputs.length })}
            </Button>
            {sets.length > 0 && (
              <Button size="small" variant="outlined" startIcon={<AddIcon />} onClick={(e) => setMenu({ anchor: e.currentTarget, domains: selectedDomains })} sx={{ whiteSpace: "nowrap" }}>
                {t("detector.sites.addSelected", { count: selectedDomains.length })}
              </Button>
            )}
            <Button size="small" onClick={() => setSelected(new Set())}>
              {t("detector.sites.clearSelection")}
            </Button>
          </Stack>
        )}
      </Stack>
      <Box sx={{ overflowX: "auto" }}>
        <Table size="small" sx={{ minWidth: 760 }}>
          <TableHead>
            <TableRow>
              <TableCell padding="checkbox">
                <Tooltip title={t("detector.sites.selectAll")}>
                  <span>
                    <Checkbox size="small" checked={allSelected} indeterminate={!allSelected && someSelected} disabled={selectable.length === 0} onChange={toggleAll} sx={checkboxSx} />
                  </span>
                </Tooltip>
              </TableCell>
              <TableCell>{t("detector.sites.site")}</TableCell>
              <TableCell>{t("detector.sites.direct")}</TableCell>
              {both && <TableCell>{t("detector.sites.throughB4Col")}</TableCell>}
              <TableCell>{t("detector.sites.outcome")}</TableCell>
              <TableCell sx={{ width: "42%" }}>{t("detector.sites.what")}</TableCell>
              <TableCell align="right" />
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.map((s) => {
              const canSelect = s.done && still(s);
              const isSelected = selected.has(rowKey(s));
              return (
                <TableRow key={rowKey(s)} selected={isSelected} sx={{ "&:last-child td": { border: 0 }, opacity: s.done ? 1 : 0.7 }}>
                  <TableCell padding="checkbox">
                    {canSelect && <Checkbox size="small" checked={isSelected} onChange={() => toggle(s)} sx={checkboxSx} />}
                  </TableCell>
                  <TableCell sx={{ fontFamily: "monospace", fontSize: "0.8rem", whiteSpace: "nowrap" }}>
                    {s.domain}
                    {s.family === "ipv6" && (
                      <Typography component="span" variant="caption" sx={{ color: colors.secondary, ml: 0.5 }}>
                        IPv6
                      </Typography>
                    )}
                    {s.url && s.url.replace(/^https:\/\/[^/]+/, "") !== "/" && (
                      <Typography component="span" variant="caption" sx={{ color: colors.text.disabled, ml: 0.5 }}>
                        {s.url.replace(/^https:\/\/[^/]+/, "").slice(0, 24)}
                      </Typography>
                    )}
                    {s.ip && (
                      <Typography variant="caption" sx={{ color: colors.text.disabled, display: "block" }}>
                        {s.ip}
                      </Typography>
                    )}
                  </TableCell>
                  <TableCell sx={{ whiteSpace: "nowrap" }}>
                    <FetchChip fetch={s.direct} />
                  </TableCell>
                  {both && (
                    <TableCell sx={{ whiteSpace: "nowrap" }}>
                      <FetchChip fetch={s.through_b4} />
                    </TableCell>
                  )}
                  <TableCell sx={{ whiteSpace: "nowrap" }}>
                    <StatusChip label={t(`detector.outcome.${s.outcome}`)} color={outcomeColor(s.outcome)} />
                  </TableCell>
                  <TableCell sx={{ color: colors.text.secondary, fontSize: "0.8rem" }}>{describe(s, t)}</TableCell>
                  <TableCell align="right" sx={{ whiteSpace: "nowrap" }}>
                    <Box sx={{ display: "inline-flex", alignItems: "center", gap: 0.5 }}>
                      {s.set_id && (
                        <Tooltip title={t("detector.sites.openSetTip", { set: s.set_name })}>
                          <IconButton size="small" onClick={() => onOpenSet(s.set_id!)} sx={iconSx}>
                            <SetsIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      )}
                      {canSelect && (
                        <>
                          <Tooltip title={t("detector.sites.discoveryTip")}>
                            <IconButton size="small" onClick={() => onDiscovery([s.input])} sx={iconSx}>
                              <DiscoveryIcon fontSize="small" />
                            </IconButton>
                          </Tooltip>
                          {!s.set_id && sets.length > 0 && (
                            <Tooltip title={t("detector.sites.addToSetTip")}>
                              <IconButton size="small" onClick={(e) => setMenu({ anchor: e.currentTarget, domains: [s.domain] })} sx={iconSx}>
                                <AddIcon fontSize="small" />
                              </IconButton>
                            </Tooltip>
                          )}
                        </>
                      )}
                    </Box>
                  </TableCell>
                </TableRow>
              );
            })}
            {rows.length === 0 && (
              <TableRow>
                <TableCell colSpan={both ? 7 : 6} sx={{ color: colors.text.secondary }}>
                  {t("detector.sites.none")}
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </Box>
      <Menu open={!!menu} anchorEl={menu?.anchor} onClose={() => setMenu(null)}>
        {sets.map((set) => (
          <MenuItem
            key={set.id}
            onClick={() => {
              if (menu) onAddToSet(set.id, menu.domains);
              setMenu(null);
            }}
          >
            {set.name}
            {!set.enabled && <B4Badge label={t("core.disabled")} size="small" sx={{ ml: 1 }} />}
          </MenuItem>
        ))}
      </Menu>
    </Stack>
  );
};
