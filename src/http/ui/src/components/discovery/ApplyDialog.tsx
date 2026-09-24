import { useEffect, useMemo, useState } from "react";
import {
  Box,
  Button,
  Checkbox,
  Chip,
  CircularProgress,
  FormControlLabel,
  Radio,
  RadioGroup,
  Stack,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { AddIcon } from "@b4.icons";
import { B4Alert, B4Select, B4TextField } from "@b4.elements";
import { B4Dialog } from "@common/B4Dialog";
import { colors } from "@design";
import { B4SetConfig } from "@models/config";
import { SimilarSet } from "@models/discovery";
import { SetDomainMatch } from "@models/sets";
import { discoveryApi } from "@api/discovery";
import { setsApi } from "@api/sets";
import {
  ApplyTarget,
  generateDomainVariants,
  pinsFor,
  probeUrlLabel,
  sanitizeProbeUrls,
  strategySuffix,
  suggestSetName,
} from "@utils";
import { StrategySummary } from "./StrategySummary";

type ApplyMode = "new" | "existing" | "replace";

const COVERING = new Set(["exact", "covered", "regexp"]);
const OTHER_SET = "__other__";

export interface ReplaceOptions {
  keepTargets: boolean;
  probeUrls?: string[];
}

interface ReplaceCandidate {
  id: string;
  name: string;
  fromRun: boolean;
  exact: boolean;
  entry?: string;
}

interface ShadowedDomain {
  domain: string;
  setName: string;
}

interface ApplyDialogProps {
  open: boolean;
  target: ApplyTarget | null;
  loading: boolean;
  runSetId?: string | null;
  sets: B4SetConfig[];
  onClose: () => void;
  onCreate: (set: B4SetConfig) => void;
  onAddToExisting: (
    setId: string,
    domains: string[],
    pins?: Record<string, string[]>,
  ) => void;
  onReplaceStrategy: (
    setId: string,
    set: B4SetConfig,
    domains: string[],
    pins: Record<string, string[]> | undefined,
    opts: ReplaceOptions,
  ) => void;
}

const lower = (d: string) => d.toLowerCase();

export const ApplyDialog = ({
  open,
  target,
  loading,
  runSetId,
  sets,
  onClose,
  onCreate,
  onAddToExisting,
  onReplaceStrategy,
}: ApplyDialogProps) => {
  const { t } = useTranslation();
  const single = target?.domains.length === 1 ? target.domains[0] : null;
  const variants = useMemo(
    () => (single ? generateDomainVariants(single) : []),
    [single],
  );

  const [name, setName] = useState("");
  const [variant, setVariant] = useState("");
  const [chosenMode, setChosenMode] = useState<ApplyMode | null>(null);
  const [pickedReplace, setPickedReplace] = useState<string | null>(null);
  const [otherSetId, setOtherSetId] = useState("");
  const [similar, setSimilar] = useState<SimilarSet[]>([]);
  const [selectedSetId, setSelectedSetId] = useState<string | null>(null);
  const [matches, setMatches] = useState<SetDomainMatch[]>([]);
  const [matchesReady, setMatchesReady] = useState(false);
  const [matchesFailed, setMatchesFailed] = useState(false);
  const [addDomains, setAddDomains] = useState(true);
  const [urlsChoice, setUrlsChoice] = useState<boolean | null>(null);

  useEffect(() => {
    if (!open || !target) return;
    setName(`${suggestSetName(target.domains[0])}${strategySuffix(target.set)}`);
    setVariant(single ? (variants[0] ?? single) : "");
    setChosenMode(null);
    setPickedReplace(null);
    setOtherSetId("");
    setSimilar([]);
    setSelectedSetId(null);
    setMatches([]);
    setAddDomains(true);
    setUrlsChoice(null);

    if (target.set.dns?.enabled) return;

    let active = true;
    discoveryApi
      .similar(target.set)
      .then((list) => {
        if (!active) return;
        const found = Array.isArray(list) ? list : [];
        setSimilar(found);
        setSelectedSetId(found[0]?.id ?? null);
      })
      .catch(() => {
        if (active) setSimilar([]);
      });
    return () => {
      active = false;
    };
  }, [open, target, single, variants]);

  const domains = useMemo(() => {
    if (!target) return [];
    return single ? [variant || single] : target.domains;
  }, [target, single, variant]);

  const tested = useMemo(() => target?.domains ?? [], [target]);

  useEffect(() => {
    setMatches([]);
    setMatchesReady(false);
    setMatchesFailed(false);
    if (!open || domains.length === 0) return;
    const query = [...new Set([...domains, ...tested].map(lower))];
    let active = true;
    setsApi
      .checkDomain(query.join(","))
      .then((found) => {
        if (!active) return;
        setMatches(Array.isArray(found) ? found : []);
        setMatchesReady(true);
      })
      .catch(() => {
        if (!active) return;
        setMatches([]);
        setMatchesFailed(true);
        setMatchesReady(true);
      });
    return () => {
      active = false;
    };
  }, [open, domains, tested]);

  const setById = useMemo(() => new Map(sets.map((s) => [s.id, s])), [sets]);

  const variantKeys = useMemo(() => new Set(domains.map(lower)), [domains]);
  const enabledMatches = useMemo(
    () => matches.filter((m) => m.enabled),
    [matches],
  );
  const claimed = enabledMatches.filter(
    (m) => variantKeys.has(lower(m.domain)) && m.relation === "exact",
  );
  const overlapping = enabledMatches.filter(
    (m) => variantKeys.has(lower(m.domain)) && m.relation !== "exact",
  );

  const candidates = useMemo(() => {
    const list: ReplaceCandidate[] = [];
    const seen = new Set<string>();
    const runSet = runSetId ? setById.get(runSetId) : undefined;
    if (runSet && !runSet.routing?.enabled) {
      list.push({ id: runSet.id, name: runSet.name, fromRun: true, exact: false });
      seen.add(runSet.id);
    }
    for (const m of enabledMatches) {
      if (!COVERING.has(m.relation) || seen.has(m.set_id)) continue;
      if (setById.get(m.set_id)?.routing?.enabled) continue;
      seen.add(m.set_id);
      list.push({
        id: m.set_id,
        name: m.set_name,
        fromRun: false,
        exact:
          m.relation === "exact" &&
          enabledMatches.some(
            (e) =>
              e.set_id === m.set_id &&
              e.relation === "exact" &&
              variantKeys.has(lower(e.domain)),
          ),
        entry: m.entry,
      });
    }
    return list;
  }, [runSetId, setById, enabledMatches, variantKeys]);

  const otherSets = useMemo(() => {
    const taken = new Set(candidates.map((c) => c.id));
    return sets.filter((s) => !s.routing?.enabled && !taken.has(s.id));
  }, [sets, candidates]);

  if (!target) return null;

  const canReplace = candidates.length > 0 || otherSets.length > 0;
  const preferReplace = candidates.some((c) => c.fromRun || c.exact);
  const wantedMode: ApplyMode =
    chosenMode ?? (preferReplace ? "replace" : "new");
  const mode: ApplyMode =
    (wantedMode === "replace" && !canReplace) ||
    (wantedMode === "existing" && similar.length === 0)
      ? "new"
      : wantedMode;

  const fallbackPick = candidates[0]?.id ?? OTHER_SET;
  const pick =
    pickedReplace === OTHER_SET && otherSets.length > 0
      ? OTHER_SET
      : (candidates.find((c) => c.id === pickedReplace)?.id ?? fallbackPick);
  const replaceSetId =
    pick === OTHER_SET
      ? (otherSets.find((s) => s.id === otherSetId)?.id ?? null)
      : pick;
  const replaceSet = replaceSetId ? setById.get(replaceSetId) : undefined;
  const replaceName =
    replaceSet?.name ??
    candidates.find((c) => c.id === replaceSetId)?.name ??
    "";

  const ownMatch = (domain: string) =>
    matches.find(
      (m) =>
        m.set_id === replaceSetId &&
        lower(m.domain) === lower(domain) &&
        COVERING.has(m.relation),
    );
  const coveredTested = tested.filter((d) => !!ownMatch(d));
  const covers = matchesFailed || coveredTested.length === tested.length;
  const missing = single
    ? domains
    : domains.filter(
        (d) => !coveredTested.some((c) => lower(c) === lower(d)),
      );
  const keepTargets = covers || !addDomains;

  const shadowed: ShadowedDomain[] = [];
  for (const domain of coveredTested) {
    const own = ownMatch(domain);
    if (!own || own.handles) continue;
    const handler = matches.find(
      (m) => lower(m.domain) === lower(domain) && m.handles,
    );
    if (handler && handler.set_id !== replaceSetId) {
      shadowed.push({ domain, setName: handler.set_name });
    }
  }

  const probeUrls = sanitizeProbeUrls(target.urls ?? []);
  const storedUrls = replaceSet?.discovery?.urls ?? [];
  const useUrls = urlsChoice ?? storedUrls.length === 0;

  const previewSet: B4SetConfig = {
    ...target.set,
    targets: { ...target.set.targets, sni_domains: domains },
  };

  const confirm = () => {
    if (mode === "existing") {
      if (selectedSetId) {
        onAddToExisting(
          selectedSetId,
          domains,
          pinsFor(target.set, target.domains),
        );
      }
      return;
    }
    if (mode === "replace") {
      if (replaceSetId) {
        onReplaceStrategy(
          replaceSetId,
          previewSet,
          keepTargets ? domains : missing,
          pinsFor(target.set, target.domains),
          {
            keepTargets,
            probeUrls: useUrls && probeUrls.length > 0 ? probeUrls : undefined,
          },
        );
      }
      return;
    }
    onCreate({
      ...previewSet,
      name: name.trim() || domains[0],
      discovery: { urls: probeUrls },
    });
  };

  const choosePick = (next: string) => {
    setPickedReplace(next);
    setUrlsChoice(null);
  };

  const chooseOther = (next: string) => {
    setOtherSetId(next);
    setUrlsChoice(null);
  };

  const selectedSimilar = similar.find((s) => s.id === selectedSetId);
  const confirmLabel =
    mode === "new"
      ? t("discovery.apply.create")
      : mode === "existing"
        ? t("discovery.apply.add")
        : t("discovery.apply.replaceAction");

  const subtitleSx = { mb: 1, color: colors.text.secondary };
  const captionSx = { color: colors.text.secondary, display: "block" };

  return (
    <B4Dialog
      open={open}
      onClose={onClose}
      title={t("discovery.apply.title")}
      subtitle={target.domains.join(", ")}
      icon={<AddIcon />}
      maxWidth="sm"
      fullWidth
      actions={
        <Stack direction="row" spacing={2}>
          <Button onClick={onClose} disabled={loading}>
            {t("core.cancel")}
          </Button>
          <Button
            variant="contained"
            onClick={confirm}
            disabled={
              loading ||
              (mode === "existing" && !selectedSetId) ||
              (mode === "replace" && (!replaceSetId || !matchesReady))
            }
            startIcon={
              loading ? (
                <CircularProgress size={18} color="inherit" />
              ) : (
                <AddIcon />
              )
            }
            sx={{ bgcolor: colors.secondary, color: colors.background.default }}
          >
            {confirmLabel}
          </Button>
        </Stack>
      }
    >
      <Stack spacing={3} sx={{ mt: 1 }}>
        {target.domains.length > 1 && (
          <B4Alert severity="info">
            {t("discovery.apply.shared", { domains: target.domains.join(", ") })}
          </B4Alert>
        )}
        <Box>
          <Typography variant="subtitle2" sx={subtitleSx}>
            {t("discovery.apply.will")}
          </Typography>
          <Box
            sx={{
              p: 1.5,
              border: `1px solid ${colors.border.light}`,
              borderRadius: 1.5,
              bgcolor: colors.background.dark,
            }}
          >
            <StrategySummary set={previewSet} preset={target.preset} compact />
          </Box>
        </Box>

        {single &&
          variants.length > 1 &&
          (mode !== "replace" || (!covers && addDomains)) && (
          <Box>
            <Typography variant="subtitle2" sx={subtitleSx}>
              {t("discovery.apply.pattern")}
            </Typography>
            <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
              {variants.map((v) => (
                <Chip
                  key={v}
                  label={v}
                  onClick={() => setVariant(v)}
                  sx={{
                    bgcolor:
                      v === variant
                        ? colors.accent.secondary
                        : colors.background.dark,
                    border:
                      v === variant
                        ? `2px solid ${colors.secondary}`
                        : `1px solid ${colors.border.default}`,
                    cursor: "pointer",
                  }}
                />
              ))}
            </Stack>
            <Typography variant="caption" sx={{ ...captionSx, mt: 0.5 }}>
              {t("discovery.apply.patternHint")}
            </Typography>
          </Box>
        )}

        {claimed.length > 0 && mode !== "replace" && (
          <B4Alert severity="warning">
            {t("discovery.apply.overlap", {
              domains: [...new Set(claimed.map((m) => m.domain))].join(", "),
              sets: [...new Set(claimed.map((m) => m.set_name))].join(", "),
            })}
          </B4Alert>
        )}
        {overlapping.length > 0 && mode !== "replace" && (
          <B4Alert severity="info">
            {t("discovery.apply.overlapCovered", {
              domains: [...new Set(overlapping.map((m) => m.domain))].join(", "),
              entries: [...new Set(overlapping.map((m) => m.entry))].join(", "),
              sets: [...new Set(overlapping.map((m) => m.set_name))].join(", "),
            })}
          </B4Alert>
        )}

        {(similar.length > 0 || canReplace) && (
          <Box>
            <Typography variant="subtitle2" sx={{ ...subtitleSx, mb: 0.5 }}>
              {t("discovery.apply.addTo")}
            </Typography>
            <RadioGroup
              value={mode}
              onChange={(e) => setChosenMode(e.target.value as ApplyMode)}
            >
              {canReplace && (
                <FormControlLabel
                  value="replace"
                  control={<Radio />}
                  label={t("discovery.apply.replaceInto")}
                />
              )}
              <FormControlLabel
                value="new"
                control={<Radio />}
                label={t("discovery.apply.createNew")}
              />
              {similar.length > 0 && (
                <FormControlLabel
                  value="existing"
                  control={<Radio />}
                  label={t("discovery.apply.addExisting", {
                    name: selectedSimilar?.name ?? similar[0].name,
                  })}
                />
              )}
            </RadioGroup>
          </Box>
        )}

        {mode === "replace" && (
          <Box>
            <Typography variant="subtitle2" sx={subtitleSx}>
              {t("discovery.apply.replaceList")}
            </Typography>
            <RadioGroup value={pick} onChange={(e) => choosePick(e.target.value)}>
              {candidates.map((c) => (
                <FormControlLabel
                  key={c.id}
                  value={c.id}
                  control={<Radio />}
                  label={
                    <Box>
                      <Typography>{c.name}</Typography>
                      <Typography variant="caption" sx={captionSx}>
                        {c.fromRun
                          ? t("discovery.apply.reasonRun")
                          : t("discovery.apply.reasonMatch", {
                              entry: c.entry,
                            })}
                      </Typography>
                    </Box>
                  }
                />
              ))}
              {otherSets.length > 0 && (
                <FormControlLabel
                  value={OTHER_SET}
                  control={<Radio />}
                  label={t("discovery.apply.otherSet")}
                />
              )}
            </RadioGroup>
            {pick === OTHER_SET && (
              <Box sx={{ mt: 1 }}>
                <B4Select
                  label={t("discovery.apply.otherSetLabel")}
                  value={otherSetId}
                  options={[
                    { value: "", label: t("discovery.apply.otherSetPick") },
                    ...otherSets.map((s) => ({
                      value: s.id,
                      label: s.enabled
                        ? s.name
                        : `${s.name} (${t("discovery.apply.disabledSet")})`,
                    })),
                  ]}
                  onChange={(e) => chooseOther(String(e.target.value))}
                />
              </Box>
            )}
          </Box>
        )}

        {mode === "replace" && replaceSetId && matchesReady && (
          <Stack spacing={1.5}>
            {matchesFailed && (
              <B4Alert severity="warning">
                {t("discovery.apply.checkFailed")}
              </B4Alert>
            )}
            {!covers && (
              <Box>
                <FormControlLabel
                  control={
                    <Checkbox
                      checked={addDomains}
                      onChange={(e) => setAddDomains(e.target.checked)}
                    />
                  }
                  label={t("discovery.apply.addDomains", {
                    domains: missing.join(", "),
                  })}
                />
                {!addDomains && (
                  <Typography variant="caption" sx={captionSx}>
                    {t("discovery.apply.addDomainsOff", {
                      domains: missing.join(", "),
                    })}
                  </Typography>
                )}
              </Box>
            )}
            {shadowed.length > 0 && (
              <B4Alert severity="warning">
                {t("discovery.apply.shadowed", {
                  domains: [...new Set(shadowed.map((s) => s.domain))].join(
                    ", ",
                  ),
                  sets: [...new Set(shadowed.map((s) => s.setName))].join(", "),
                  name: replaceName,
                })}
              </B4Alert>
            )}
            {replaceSet && !replaceSet.enabled && (
              <B4Alert severity="info">
                {t("discovery.apply.setDisabled", { name: replaceName })}
              </B4Alert>
            )}
            {probeUrls.length > 0 && (
              <Box>
                <FormControlLabel
                  control={
                    <Checkbox
                      checked={useUrls}
                      onChange={(e) => setUrlsChoice(e.target.checked)}
                    />
                  }
                  label={t("discovery.apply.useUrls")}
                />
                <Typography variant="caption" sx={captionSx}>
                  {probeUrls.map(probeUrlLabel).join(", ")}
                  {storedUrls.length > 0 &&
                    ` · ${t("discovery.apply.urlsStored", { count: storedUrls.length })}`}
                </Typography>
              </Box>
            )}
            <Typography variant="caption" sx={captionSx}>
              {t("discovery.apply.replaceNote")}
              {replaceSet?.hub?.id && ` ${t("discovery.apply.replaceHub")}`}
            </Typography>
          </Stack>
        )}

        {mode === "new" && (
          <B4TextField
            label={t("discovery.apply.name")}
            value={name}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
              setName(e.target.value)
            }
            fullWidth
          />
        )}

        {mode === "existing" && similar.length > 1 && (
          <Box>
            <Typography variant="subtitle2" sx={subtitleSx}>
              {t("discovery.apply.existingList")}
            </Typography>
            <RadioGroup
              value={selectedSetId ?? ""}
              onChange={(e) => setSelectedSetId(e.target.value)}
            >
              {similar.map((set) => (
                <FormControlLabel
                  key={set.id}
                  value={set.id}
                  control={<Radio />}
                  sx={{
                    alignItems: "flex-start",
                    mx: 0,
                    mb: 0.5,
                    p: 1,
                    borderRadius: 1,
                    border: `1px solid ${
                      set.id === selectedSetId
                        ? colors.secondary
                        : colors.border.default
                    }`,
                    bgcolor:
                      set.id === selectedSetId
                        ? colors.accent.secondary
                        : colors.background.dark,
                  }}
                  label={
                    <Box>
                      <Typography sx={{ fontWeight: 600 }}>
                        {set.name}
                      </Typography>
                      <Typography
                        variant="caption"
                        sx={{ color: colors.text.secondary }}
                      >
                        {set.domains.slice(0, 3).join(", ")}
                        {set.domains.length > 3 &&
                          ` ${t("discovery.apply.more", { count: set.domains.length - 3 })}`}
                      </Typography>
                    </Box>
                  }
                />
              ))}
            </RadioGroup>
          </Box>
        )}
      </Stack>
    </B4Dialog>
  );
};
