import { useMemo, useState } from "react";
import {
  Box,
  Button,
  Menu,
  MenuItem,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
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
  onAddToSet: (setId: string, domain: string) => void;
}

const problem = (s: SiteResult) => ["fixed", "still_blocked", "blocked", "broken_by_b4"].includes(s.outcome);
const still = (s: SiteResult) => ["still_blocked", "blocked", "broken_by_b4"].includes(s.outcome);

function describe(s: SiteResult, t: (k: string, o?: Record<string, unknown>) => string): string {
  const parts: string[] = [];
  const d = s.direct;
  if (!d || d.status === "CHECKING" || d.status === "PENDING") return t("detector.sites.checking");
  if (d.detail) parts.push(d.detail);
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
  const [menu, setMenu] = useState<{ anchor: HTMLElement; domain: string } | null>(null);

  const counts = useMemo(
    () => ({
      all: (result.sites ?? []).length,
      problems: (result.sites ?? []).filter(problem).length,
      still: (result.sites ?? []).filter(still).length,
    }),
    [result.sites],
  );
  const rows = (result.sites ?? []).filter((s) => (filter === "all" ? true : filter === "problems" ? problem(s) : still(s)));

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
        {result.stub_ips && result.stub_ips.length > 0 && (
          <Typography variant="caption" sx={{ color: colors.text.secondary, ml: "auto !important" }}>
            {t("detector.sites.stubs", { ips: result.stub_ips.join(", ") })}
          </Typography>
        )}
      </Stack>
      <Box sx={{ overflowX: "auto" }}>
        <Table size="small" sx={{ minWidth: 720 }}>
          <TableHead>
            <TableRow>
              <TableCell>{t("detector.sites.site")}</TableCell>
              <TableCell>{t("detector.sites.direct")}</TableCell>
              {both && <TableCell>{t("detector.sites.throughB4Col")}</TableCell>}
              <TableCell>{t("detector.sites.outcome")}</TableCell>
              <TableCell sx={{ width: "42%" }}>{t("detector.sites.what")}</TableCell>
              <TableCell align="right" />
            </TableRow>
          </TableHead>
          <TableBody>
            {rows.map((s) => (
              <TableRow key={s.input} sx={{ "&:last-child td": { border: 0 }, opacity: s.done ? 1 : 0.7 }}>
                <TableCell sx={{ fontFamily: "monospace", fontSize: "0.8rem", whiteSpace: "nowrap" }}>
                  {s.domain}
                  {s.url && s.url.replace(/^https:\/\/[^/]+/, "") !== "/" && (
                    <Typography component="span" variant="caption" sx={{ color: colors.text.disabled, ml: 0.5 }}>
                      {s.url.replace(/^https:\/\/[^/]+/, "").slice(0, 24)}
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
                  {s.set_id && (
                    <Button size="small" onClick={() => onOpenSet(s.set_id!)} sx={{ textTransform: "none" }}>
                      {t("detector.sites.openSet", { set: s.set_name })}
                    </Button>
                  )}
                  {still(s) && s.done && (
                    <>
                      <Button size="small" onClick={() => onDiscovery([s.input])}>
                        {t("detector.sites.discovery")}
                      </Button>
                      {!s.set_id && sets.length > 0 && (
                        <Button size="small" onClick={(e) => setMenu({ anchor: e.currentTarget, domain: s.domain })}>
                          {t("detector.sites.addToSet")}
                        </Button>
                      )}
                    </>
                  )}
                </TableCell>
              </TableRow>
            ))}
            {rows.length === 0 && (
              <TableRow>
                <TableCell colSpan={both ? 6 : 5} sx={{ color: colors.text.secondary }}>
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
              if (menu) onAddToSet(set.id, menu.domain);
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
