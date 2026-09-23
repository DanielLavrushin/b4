import { useEffect, useMemo, useState } from "react";
import {
  Box,
  Button,
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
import { B4Alert, B4TextField } from "@b4.elements";
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
  strategySuffix,
  suggestSetName,
} from "@utils";
import { StrategySummary } from "./StrategySummary";

type ApplyMode = "new" | "existing" | "replace";

interface ApplyDialogProps {
  open: boolean;
  target: ApplyTarget | null;
  loading: boolean;
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
    pins?: Record<string, string[]>,
  ) => void;
}

export const ApplyDialog = ({
  open,
  target,
  loading,
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
  const [pickedReplaceId, setPickedReplaceId] = useState<string | null>(null);
  const [similar, setSimilar] = useState<SimilarSet[]>([]);
  const [selectedSetId, setSelectedSetId] = useState<string | null>(null);
  const [claimed, setClaimed] = useState<SetDomainMatch[]>([]);
  const [covered, setCovered] = useState<SetDomainMatch[]>([]);

  useEffect(() => {
    if (!open || !target) return;
    setName(`${suggestSetName(target.domains[0])}${strategySuffix(target.set)}`);
    setVariant(single ? (variants[0] ?? single) : "");
    setChosenMode(null);
    setPickedReplaceId(null);
    setSimilar([]);
    setSelectedSetId(null);
    setClaimed([]);
    setCovered([]);

    if (target.set.dns?.enabled) return;

    let active = true;
    discoveryApi
      .similar(target.set)
      .then((sets) => {
        if (!active) return;
        const list = Array.isArray(sets) ? sets : [];
        setSimilar(list);
        setSelectedSetId(list[0]?.id ?? null);
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

  useEffect(() => {
    setClaimed([]);
    setCovered([]);
    if (!open || domains.length === 0) return;
    let active = true;
    setsApi
      .checkDomain(domains.join(","))
      .then((matches) => {
        if (!active) return;
        const enabled = Array.isArray(matches)
          ? matches.filter((m) => m.enabled)
          : [];
        setClaimed(enabled.filter((m) => m.relation === "exact"));
        setCovered(enabled.filter((m) => m.relation !== "exact"));
      })
      .catch(() => {
        if (active) {
          setClaimed([]);
          setCovered([]);
        }
      });
    return () => {
      active = false;
    };
  }, [open, domains]);

  const replaceTargets = useMemo(() => {
    const seen = new Map<string, string>();
    for (const m of claimed) {
      if (!seen.has(m.set_id)) seen.set(m.set_id, m.set_name);
    }
    return [...seen].map(([id, name]) => ({ id, name }));
  }, [claimed]);

  const wantedMode: ApplyMode =
    chosenMode ?? (replaceTargets.length > 0 ? "replace" : "new");
  const mode: ApplyMode =
    (wantedMode === "replace" && replaceTargets.length === 0) ||
    (wantedMode === "existing" && similar.length === 0)
      ? "new"
      : wantedMode;
  const replaceSetId =
    replaceTargets.find((s) => s.id === pickedReplaceId)?.id ??
    replaceTargets[0]?.id ??
    null;

  if (!target) return null;

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
          domains,
          pinsFor(target.set, target.domains),
        );
      }
      return;
    }
    onCreate({ ...previewSet, name: name.trim() || domains[0] });
  };

  const chooseMode = (next: ApplyMode) => setChosenMode(next);

  const selectedSimilar = similar.find((s) => s.id === selectedSetId);
  const selectedReplace = replaceTargets.find((s) => s.id === replaceSetId);
  const confirmLabel =
    mode === "new"
      ? t("discovery.apply.create")
      : mode === "existing"
        ? t("discovery.apply.add")
        : t("discovery.apply.replaceAction");

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
              (mode === "replace" && !replaceSetId)
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
          <Typography
            variant="subtitle2"
            sx={{ mb: 1, color: colors.text.secondary }}
          >
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

        {single && variants.length > 1 && (
          <Box>
            <Typography
              variant="subtitle2"
              sx={{ mb: 1, color: colors.text.secondary }}
            >
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
            <Typography
              variant="caption"
              sx={{ color: colors.text.secondary, display: "block", mt: 0.5 }}
            >
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
        {covered.length > 0 && (
          <B4Alert severity="info">
            {t("discovery.apply.overlapCovered", {
              domains: [...new Set(covered.map((m) => m.domain))].join(", "),
              entries: [...new Set(covered.map((m) => m.entry))].join(", "),
              sets: [...new Set(covered.map((m) => m.set_name))].join(", "),
            })}
          </B4Alert>
        )}

        {(similar.length > 0 || replaceTargets.length > 0) && (
          <Box>
            <Typography
              variant="subtitle2"
              sx={{ mb: 0.5, color: colors.text.secondary }}
            >
              {t("discovery.apply.addTo")}
            </Typography>
            <RadioGroup
              value={mode}
              onChange={(e) => chooseMode(e.target.value as ApplyMode)}
            >
              {replaceTargets.length > 0 && (
                <FormControlLabel
                  value="replace"
                  control={<Radio />}
                  label={t("discovery.apply.replaceIn", {
                    name: selectedReplace?.name,
                  })}
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
            {mode === "replace" && (
              <Typography
                variant="caption"
                sx={{ color: colors.text.secondary, display: "block" }}
              >
                {t("discovery.apply.replaceHint")}
              </Typography>
            )}
          </Box>
        )}

        {mode === "replace" && replaceTargets.length > 1 && (
          <Box>
            <Typography
              variant="subtitle2"
              sx={{ mb: 1, color: colors.text.secondary }}
            >
              {t("discovery.apply.replaceList")}
            </Typography>
            <RadioGroup
              value={replaceSetId ?? ""}
              onChange={(e) => setPickedReplaceId(e.target.value)}
            >
              {replaceTargets.map((set) => (
                <FormControlLabel
                  key={set.id}
                  value={set.id}
                  control={<Radio />}
                  label={set.name}
                />
              ))}
            </RadioGroup>
          </Box>
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
            <Typography
              variant="subtitle2"
              sx={{ mb: 1, color: colors.text.secondary }}
            >
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
