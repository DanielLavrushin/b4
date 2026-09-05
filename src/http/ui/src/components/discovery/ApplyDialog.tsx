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
}

export const ApplyDialog = ({
  open,
  target,
  loading,
  onClose,
  onCreate,
  onAddToExisting,
}: ApplyDialogProps) => {
  const { t } = useTranslation();
  const single = target?.domains.length === 1 ? target.domains[0] : null;
  const variants = useMemo(
    () => (single ? generateDomainVariants(single) : []),
    [single],
  );

  const [name, setName] = useState("");
  const [variant, setVariant] = useState("");
  const [mode, setMode] = useState<"new" | "existing">("new");
  const [similar, setSimilar] = useState<SimilarSet[]>([]);
  const [selectedSetId, setSelectedSetId] = useState<string | null>(null);
  const [claimed, setClaimed] = useState<SetDomainMatch[]>([]);

  useEffect(() => {
    if (!open || !target) return;
    setName(`${suggestSetName(target.domains[0])}${strategySuffix(target.set)}`);
    setVariant(single ? (variants[0] ?? single) : "");
    setMode("new");
    setSimilar([]);
    setSelectedSetId(null);
    setClaimed([]);

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
    if (!open || domains.length === 0) {
      setClaimed([]);
      return;
    }
    let active = true;
    setsApi
      .checkDomain(domains.join(","))
      .then((matches) => {
        if (!active) return;
        setClaimed(
          Array.isArray(matches)
            ? matches.filter((m) => m.enabled && m.relation === "exact")
            : [],
        );
      })
      .catch(() => {
        if (active) setClaimed([]);
      });
    return () => {
      active = false;
    };
  }, [open, domains]);

  if (!target) return null;

  const previewSet: B4SetConfig = {
    ...target.set,
    targets: { ...target.set.targets, sni_domains: domains },
  };

  const confirm = () => {
    if (mode === "existing") {
      if (selectedSetId) {
        onAddToExisting(selectedSetId, domains, pinsFor(target.set, domains));
      }
      return;
    }
    onCreate({ ...previewSet, name: name.trim() || domains[0] });
  };

  const selectedSimilar = similar.find((s) => s.id === selectedSetId);

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
            disabled={loading || (mode === "existing" && !selectedSetId)}
            startIcon={
              loading ? (
                <CircularProgress size={18} color="inherit" />
              ) : (
                <AddIcon />
              )
            }
            sx={{ bgcolor: colors.secondary, color: colors.background.default }}
          >
            {mode === "new"
              ? t("discovery.apply.create")
              : t("discovery.apply.add")}
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

        {claimed.length > 0 && (
          <B4Alert severity="warning">
            {t("discovery.apply.overlap", {
              domains: [...new Set(claimed.map((m) => m.domain))].join(", "),
              sets: [...new Set(claimed.map((m) => m.set_name))].join(", "),
            })}
          </B4Alert>
        )}

        {similar.length > 0 && (
          <Box>
            <Typography
              variant="subtitle2"
              sx={{ mb: 0.5, color: colors.text.secondary }}
            >
              {t("discovery.apply.addTo")}
            </Typography>
            <RadioGroup
              value={mode}
              onChange={(e) => setMode(e.target.value as "new" | "existing")}
            >
              <FormControlLabel
                value="new"
                control={<Radio />}
                label={t("discovery.apply.createNew")}
              />
              <FormControlLabel
                value="existing"
                control={<Radio />}
                label={t("discovery.apply.addExisting", {
                  name: selectedSimilar?.name ?? similar[0].name,
                })}
              />
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
            <Stack spacing={1}>
              {similar.map((set) => (
                <Box
                  key={set.id}
                  onClick={() => setSelectedSetId(set.id)}
                  sx={{
                    p: 1.5,
                    borderRadius: 1,
                    cursor: "pointer",
                    bgcolor:
                      set.id === selectedSetId
                        ? colors.accent.secondary
                        : colors.background.dark,
                    border:
                      set.id === selectedSetId
                        ? `2px solid ${colors.secondary}`
                        : `1px solid ${colors.border.default}`,
                  }}
                >
                  <Typography sx={{ fontWeight: 600 }}>{set.name}</Typography>
                  <Typography
                    variant="caption"
                    sx={{ color: colors.text.secondary }}
                  >
                    {set.domains.slice(0, 3).join(", ")}
                    {set.domains.length > 3 &&
                      ` ${t("discovery.apply.more", { count: set.domains.length - 3 })}`}
                  </Typography>
                </Box>
              ))}
            </Stack>
          </Box>
        )}
      </Stack>
    </B4Dialog>
  );
};
