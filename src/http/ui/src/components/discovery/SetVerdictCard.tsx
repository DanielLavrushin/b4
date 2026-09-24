import { ReactNode, useEffect, useMemo, useState } from "react";
import {
  Box,
  Button,
  CircularProgress,
  Stack,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { AddIcon, DeleteIcon } from "@b4.icons";
import { colors } from "@design";
import { B4Badge, B4ResultCard, B4ResultStatus } from "@b4.elements";
import { B4SetConfig } from "@models/config";
import {
  DiscoverySuite,
  HistoryEntry,
  SetVerdict,
} from "@models/discovery";
import { SetDomainMatch } from "@models/sets";
import { setsApi } from "@api/sets";
import {
  SET_CURRENT_PRESET,
  appliedMarks,
  normalizeProbeUrl,
  normalizeSet,
  pinsFor,
  probeUrlLabel,
  sanitizeProbeUrls,
  suiteCheckUrls,
} from "@utils";
import { StrategySummary } from "./StrategySummary";
import { VerdictApplyDialog } from "./VerdictApplyDialog";

export interface VerdictApply {
  setId: string;
  set: B4SetConfig;
  domains: string[];
  pins?: Record<string, string[]>;
  probeUrls?: string[];
  preset: string;
}

interface SetVerdictCardProps {
  suite: DiscoverySuite;
  verdict: SetVerdict;
  set?: B4SetConfig;
  setName: string;
  history: HistoryEntry[];
  applying: boolean;
  onApply: (req: VerdictApply) => Promise<boolean>;
  onSaveUrls: (setId: string, urls: string[], removed: string[]) => Promise<void>;
}

interface ForeignHandler {
  domain: string;
  owner: string;
}

const lower = (d: string) => d.toLowerCase();

export const SetVerdictCard = ({
  suite,
  verdict,
  set,
  setName,
  history,
  applying,
  onApply,
  onSaveUrls,
}: SetVerdictCardProps) => {
  const { t } = useTranslation();
  const [dialogOpen, setDialogOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const [handlers, setHandlers] = useState<SetDomainMatch[]>([]);

  const setId = suite.set_id ?? "";
  const covered = useMemo(() => verdict.covered ?? [], [verdict.covered]);
  const uncovered = useMemo(() => verdict.uncovered ?? [], [verdict.uncovered]);
  const noBypass = verdict.no_bypass ?? [];
  const runDomains = useMemo(
    () => (suite.domains ?? []).map((d) => d.domain),
    [suite.domains],
  );
  const total = runDomains.length || covered.length;
  const preset = verdict.winner_preset ?? "";
  const strategy = useMemo(
    () => (verdict.set ? normalizeSet(verdict.set) : null),
    [verdict.set],
  );
  const checkUrls = useMemo(
    () => sanitizeProbeUrls(suiteCheckUrls(suite)),
    [suite],
  );
  const addresses = covered.length > 0 ? covered : runDomains;
  const addressKey = addresses.join(",");
  const mark = preset ? appliedMarks(history, covered)[preset] : undefined;
  const tried = !!mark && mark.set_id === setId;

  useEffect(() => {
    setHandlers([]);
    if (verdict.status !== "current_works" || !addressKey) return;
    let active = true;
    setsApi
      .checkDomain(addressKey)
      .then((found) => {
        if (active) setHandlers(Array.isArray(found) ? found : []);
      })
      .catch(() => {
        if (active) setHandlers([]);
      });
    return () => {
      active = false;
    };
  }, [verdict.status, addressKey]);

  const foreign = useMemo(() => {
    const out: ForeignHandler[] = [];
    for (const domain of addressKey ? addressKey.split(",") : []) {
      const m = handlers.find(
        (h) => h.handles && lower(h.domain) === lower(domain),
      );
      if (m && m.set_id !== setId) {
        out.push({ domain, owner: m.set_name || m.set_id });
      }
    }
    return out;
  }, [handlers, addressKey, setId]);

  const unmatched = useMemo(() => {
    if (handlers.length === 0) return [];
    return (addressKey ? addressKey.split(",") : []).filter(
      (domain) =>
        !handlers.some((h) => h.handles && lower(h.domain) === lower(domain)),
    );
  }, [handlers, addressKey]);

  const uncoveredHosts = useMemo(() => {
    const hosts = new Set<string>();
    const urlOf = new Map(
      (suite.domains ?? []).map((d) => [lower(d.domain), d.check_url]),
    );
    for (const domain of uncovered) {
      hosts.add(lower(domain));
      const url = urlOf.get(lower(domain));
      const host = url ? normalizeProbeUrl(url)?.host : undefined;
      if (host) hosts.add(host);
    }
    return hosts;
  }, [suite.domains, uncovered]);

  const storedUrls = set?.discovery?.urls ?? [];
  const baseUrls = storedUrls.length > 0 ? storedUrls : checkUrls;
  const keptUrls = baseUrls.filter((u) => {
    const host = normalizeProbeUrl(u)?.host;
    return !host || !uncoveredHosts.has(host);
  });
  const canPrune =
    !!set && uncovered.length > 0 && keptUrls.length < baseUrls.length;

  const plainFix =
    verdict.family === "alt_address" || verdict.family === "dns_redirect";
  const canApply =
    !!set &&
    !!strategy &&
    covered.length > 0 &&
    !(
      verdict.status === "partial" &&
      (preset === SET_CURRENT_PRESET || plainFix)
    );

  const confirm = async (useUrls: boolean) => {
    if (!set || !strategy) return;
    const ok = await onApply({
      setId: set.id,
      set: strategy,
      domains: covered,
      pins: pinsFor(strategy, covered),
      probeUrls: useUrls && checkUrls.length > 0 ? checkUrls : undefined,
      preset,
    });
    if (ok) setDialogOpen(false);
  };

  const prune = async () => {
    if (!set) return;
    setSaving(true);
    await onSaveUrls(set.id, keptUrls, uncovered);
    setSaving(false);
  };

  const muted = { color: colors.text.secondary };
  const stoppedNote = suite.stopped_covered ? (
    <Typography variant="caption" sx={{ ...muted, display: "block" }}>
      {t("discovery.verdict.stoppedCovered")}
    </Typography>
  ) : null;
  const missingSet = !set ? (
    <Typography variant="caption" sx={{ ...muted, display: "block" }}>
      {t("discovery.verdict.missingSet")}
    </Typography>
  ) : null;

  const applyButton = (label: string) => (
    <Button
      variant="contained"
      startIcon={
        applying ? <CircularProgress size={18} color="inherit" /> : <AddIcon />
      }
      disabled={applying || saving || !canApply}
      onClick={() => setDialogOpen(true)}
      sx={{
        bgcolor: colors.secondary,
        color: colors.background.default,
        "&:hover": { bgcolor: colors.primary },
        whiteSpace: "nowrap",
      }}
    >
      {label}
    </Button>
  );

  const strategyBox = (actions: ReactNode) =>
    strategy ? (
      <Box
        sx={{
          display: "flex",
          gap: 2,
          alignItems: "flex-start",
          flexWrap: "wrap",
          p: 1.5,
          border: `1px solid ${colors.border.light}`,
          borderRadius: 1.5,
          bgcolor: colors.background.dark,
        }}
      >
        <Box sx={{ flex: "1 1 320px", minWidth: 0 }}>
          <StrategySummary
            set={strategy}
            preset={preset || undefined}
            domains={covered}
          />
        </Box>
        <Stack spacing={1} alignItems="flex-end" sx={{ flexShrink: 0 }}>
          {actions}
        </Stack>
      </Box>
    ) : null;

  const triedBadge = tried ? (
    <B4Badge
      variant="outlined"
      color="secondary"
      label={t("discovery.results.tried")}
    />
  ) : null;

  let status: B4ResultStatus = "neutral";
  let title = "";
  let subtitle: ReactNode;
  let badge: ReactNode;
  let body: ReactNode;

  switch (verdict.status) {
    case "covered":
      status = "ok";
      title = t("discovery.verdict.covered.title", {
        count: total,
        name: setName,
      });
      subtitle = covered.join(", ");
      badge = (
        <>
          <B4Badge
            variant="outlined"
            color={verdict.confirmed ? "success" : "warning"}
            label={
              verdict.confirmed
                ? t("discovery.verdict.confirmed")
                : t("discovery.status.notConfirmed")
            }
          />
          {triedBadge}
        </>
      );
      body = (
        <Stack spacing={1.5}>
          {strategyBox(
            applyButton(t("discovery.verdict.apply", { name: setName })),
          )}
          {noBypass.length > 0 && (
            <Typography variant="caption" sx={{ ...muted, display: "block" }}>
              {t("discovery.verdict.covered.noBypass", {
                domains: noBypass.join(", "),
              })}
            </Typography>
          )}
          {missingSet}
          {stoppedNote}
        </Stack>
      );
      break;
    case "current_works":
      status = "ok";
      title = t("discovery.verdict.currentWorks.title", { name: setName });
      subtitle = t("discovery.verdict.currentWorks.body");
      badge = (
        <B4Badge
          variant="outlined"
          color="success"
          label={t("discovery.verdict.status.current_works")}
        />
      );
      body = (
        <Stack spacing={0.75}>
          <Typography variant="body2" sx={muted}>
            {t("discovery.verdict.currentWorks.stillFails")}
          </Typography>
          {set && !set.enabled && (
            <Typography variant="body2" sx={muted}>
              {t("discovery.apply.setDisabled", { name: set.name })}
            </Typography>
          )}
          <Box component="ul" sx={{ m: 0, pl: 2.5, ...muted }}>
            {foreign.map((f) => (
              <Typography component="li" variant="body2" key={f.domain}>
                {t("discovery.verdict.currentWorks.otherSet", {
                  domain: f.domain,
                  owner: f.owner,
                  name: setName,
                })}
              </Typography>
            ))}
            {unmatched.map((domain) => (
              <Typography component="li" variant="body2" key={domain}>
                {t("discovery.verdict.currentWorks.unmatched", {
                  domain,
                  name: setName,
                })}
              </Typography>
            ))}
            <Typography component="li" variant="body2">
              {t("discovery.verdict.currentWorks.quic")}
            </Typography>
          </Box>
          {stoppedNote}
        </Stack>
      );
      break;
    case "partial":
      status = "warning";
      title = t("discovery.verdict.partial.title", { name: setName });
      subtitle = (
        <>
          {covered.length > 0 && (
            <Box component="span" sx={{ display: "block" }}>
              {t("discovery.verdict.partial.covers", {
                domains: covered.join(", "),
              })}
            </Box>
          )}
          {uncovered.length > 0 && (
            <Box component="span" sx={{ display: "block" }}>
              {t("discovery.verdict.partial.misses", {
                domains: uncovered.join(", "),
              })}
            </Box>
          )}
        </>
      );
      badge = (
        <>
          <B4Badge
            variant="outlined"
            color="warning"
            label={t("discovery.verdict.status.partial")}
          />
          {triedBadge}
        </>
      );
      body = (
        <Stack spacing={1.5}>
          {strategyBox(
            <>
              {applyButton(
                t("discovery.verdict.partial.applyAnyway", { name: setName }),
              )}
              {canPrune && (
                <Button
                  size="small"
                  startIcon={
                    saving ? (
                      <CircularProgress size={14} color="inherit" />
                    ) : (
                      <DeleteIcon />
                    )
                  }
                  disabled={applying || saving}
                  onClick={() => void prune()}
                  sx={{ textTransform: "none", whiteSpace: "nowrap" }}
                >
                  {t("discovery.verdict.partial.prune")}
                </Button>
              )}
            </>,
          )}
          {(preset === SET_CURRENT_PRESET || plainFix) && (
            <Typography variant="caption" sx={{ ...muted, display: "block" }}>
              {t("discovery.verdict.partial.noApply")}
            </Typography>
          )}
          {canPrune && (
            <Typography variant="caption" sx={{ ...muted, display: "block" }}>
              {keptUrls.length > 0
                ? t("discovery.verdict.partial.pruneKeeps", {
                    urls: keptUrls.map(probeUrlLabel).join(", "),
                  })
                : t("discovery.verdict.partial.pruneEmpty")}
            </Typography>
          )}
          <Typography variant="caption" sx={{ ...muted, display: "block" }}>
            {t("discovery.verdict.partial.separate")}
          </Typography>
          {missingSet}
        </Stack>
      );
      break;
    case "not_needed":
      status = "neutral";
      title = t("discovery.verdict.notNeeded.title");
      subtitle = t("discovery.verdict.notNeeded.body");
      badge = (
        <B4Badge
          variant="outlined"
          label={t("discovery.verdict.status.not_needed")}
        />
      );
      break;
    case "none":
      status = "error";
      title = t("discovery.verdict.none.title");
      subtitle = t("discovery.verdict.none.body");
      badge = (
        <B4Badge
          variant="outlined"
          color="error"
          label={t("discovery.verdict.status.none")}
        />
      );
      break;
    default:
      status = "neutral";
      title = t("discovery.verdict.incomplete.title");
      subtitle = t("discovery.verdict.incomplete.body");
      badge = (
        <B4Badge
          variant="outlined"
          color="warning"
          label={t("discovery.verdict.status.incomplete")}
        />
      );
  }

  return (
    <>
      <B4ResultCard status={status} title={title} subtitle={subtitle} badge={badge}>
        {body}
      </B4ResultCard>
      {dialogOpen && set && strategy && (
        <VerdictApplyDialog
          set={set}
          strategy={strategy}
          preset={preset}
          domains={covered}
          uncovered={verdict.status === "partial" ? uncovered : []}
          probeUrls={checkUrls}
          loading={applying}
          onClose={() => setDialogOpen(false)}
          onConfirm={(useUrls) => void confirm(useUrls)}
        />
      )}
    </>
  );
};
