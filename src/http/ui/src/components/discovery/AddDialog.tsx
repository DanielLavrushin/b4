import { useState, useEffect } from "react";
import {
  Stack,
  Typography,
  Button,
  RadioGroup,
  FormControlLabel,
  Radio,
  Box,
  Chip,
  CircularProgress,
} from "@mui/material";
import { AddIcon } from "@b4.icons";
import { B4TextField, B4Dialog, B4Alert } from "@b4.elements";
import { colors } from "@design";
import { B4SetConfig } from "@models/config";
import { SetDomainMatch } from "@models/sets";
import { generateDomainVariants } from "@utils";
import { useTranslation } from "react-i18next";

interface SimilarSet {
  id: string;
  name: string;
  domains: string[];
}

interface DiscoveryAddDialogProps {
  open: boolean;
  domain: string;
  domains?: string[];
  presetName: string;
  setConfig: B4SetConfig | null;
  onClose: () => void;
  onAddNew: (name: string, domain: string, allDomains?: string[]) => void;
  onAddToExisting: (setId: string, domain: string, allDomains?: string[]) => void;
  loading?: boolean;
}

export const DiscoveryAddDialog = ({
  open,
  domain,
  domains,
  presetName,
  setConfig,
  onClose,
  onAddNew,
  onAddToExisting,
  loading = false,
}: DiscoveryAddDialogProps) => {
  const { t } = useTranslation();
  const isMultiDomain = (domains?.length ?? 0) > 1;
  const [name, setName] = useState(presetName);
  const [variants, setVariants] = useState<string[]>([]);
  const [selectedVariant, setSelectedVariant] = useState(domain);
  const [mode, setMode] = useState<"new" | "existing">("new");
  const [similarSets, setSimilarSets] = useState<SimilarSet[]>([]);
  const [selectedSetId, setSelectedSetId] = useState<string | null>(null);
  const [claimedElsewhere, setClaimedElsewhere] = useState<SetDomainMatch[]>([]);

  useEffect(() => {
    if (open && domain) {
      if (!isMultiDomain) {
        const v = generateDomainVariants(domain);
        setVariants(v);
        setSelectedVariant(v[0] || domain);
      }
      setName(presetName);
      setMode("new");
      setSelectedSetId(null);
    }
  }, [open, domain, presetName, isMultiDomain]);

  // Fetch similar sets when dialog opens
  useEffect(() => {
    if (!open || !setConfig) return;

    const fetchSimilar = async () => {
      try {
        const response = await fetch("/api/discovery/similar", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(setConfig),
        });
        if (response.ok) {
          const data = (await response.json()) as SimilarSet[];
          setSimilarSets(data);
          if (data.length > 0) {
            setSelectedSetId(data[0].id);
          }
        }
      } catch {
        setSimilarSets([]);
      }
    };

    void fetchSimilar();
  }, [open, setConfig]);

  useEffect(() => {
    if (!open) {
      setClaimedElsewhere([]);
      return;
    }

    const targets = isMultiDomain ? domains! : [selectedVariant];
    const query = targets.filter(Boolean).join(",");
    if (!query) {
      setClaimedElsewhere([]);
      return;
    }

    let active = true;
    fetch(`/api/sets/check-domain?domain=${encodeURIComponent(query)}`)
      .then((res) => res.json())
      .then((matches: SetDomainMatch[]) => {
        if (!active) return;
        setClaimedElsewhere(
          Array.isArray(matches)
            ? matches.filter((m) => m.enabled && m.relation === "exact")
            : [],
        );
      })
      .catch(() => {
        if (active) setClaimedElsewhere([]);
      });

    return () => {
      active = false;
    };
  }, [open, isMultiDomain, domains, selectedVariant]);

  const handleConfirm = () => {
    if (mode === "new") {
      onAddNew(name, isMultiDomain ? domains![0] : selectedVariant, isMultiDomain ? domains : undefined);
    } else if (selectedSetId) {
      onAddToExisting(selectedSetId, isMultiDomain ? domains![0] : selectedVariant, isMultiDomain ? domains : undefined);
    }
  };

  return (
    <B4Dialog
      open={open}
      onClose={onClose}
      title={t("discovery.addDialog.title")}
      subtitle={t("discovery.addDialog.subtitle", { name: presetName })}
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
            onClick={handleConfirm}
            disabled={loading || (mode === "existing" && !selectedSetId)}
            startIcon={loading ? <CircularProgress size={18} /> : <AddIcon />}
            sx={{ bgcolor: colors.secondary }}
          >
            {mode === "new" ? t("discovery.addDialog.createSet") : t("discovery.addDialog.addToSet")}
          </Button>
        </Stack>
      }
    >
      <Stack spacing={3} sx={{ mt: 1 }}>
        {isMultiDomain ? (
          <Box>
            <Typography
              variant="subtitle2"
              sx={{ mb: 1, color: colors.text.secondary }}
            >
              {t("discovery.addDialog.domainsIncluded", { count: domains!.length })}
            </Typography>
            <Stack direction="row" spacing={1} flexWrap="wrap" gap={1}>
              {domains!.map((d) => (
                <Chip
                  key={d}
                  label={d}
                  sx={{
                    bgcolor: colors.accent.secondary,
                    border: `1px solid ${colors.secondary}`,
                  }}
                />
              ))}
            </Stack>
          </Box>
        ) : (
          <Box>
            <Typography
              variant="subtitle2"
              sx={{ mb: 1, color: colors.text.secondary }}
            >
              {t("discovery.addDialog.domainPattern")}
            </Typography>
            <Stack direction="row" spacing={1} flexWrap="wrap" gap={1}>
              {variants.map((v) => (
                <Chip
                  key={v}
                  label={v}
                  onClick={() => setSelectedVariant(v)}
                  sx={{
                    bgcolor:
                      v === selectedVariant
                        ? colors.accent.secondary
                        : colors.background.dark,
                    border:
                      v === selectedVariant
                        ? `2px solid ${colors.secondary}`
                        : `1px solid ${colors.border.default}`,
                    cursor: "pointer",
                  }}
                />
              ))}
            </Stack>
          </Box>
        )}

        {claimedElsewhere.length > 0 && (
          <B4Alert severity="warning">
            {t("discovery.addDialog.overlapWarning", {
              domains: [...new Set(claimedElsewhere.map((m) => m.domain))].join(
                ", ",
              ),
              sets: [...new Set(claimedElsewhere.map((m) => m.set_name))].join(
                ", ",
              ),
            })}
          </B4Alert>
        )}

        {/* Mode selection - only show if similar sets exist */}
        {similarSets.length > 0 && (
          <Box>
            <Typography
              variant="subtitle2"
              sx={{ mb: 1, color: colors.text.secondary }}
            >
              {t("discovery.addDialog.addTo")}
            </Typography>
            <RadioGroup
              value={mode}
              onChange={(e) => setMode(e.target.value as "new" | "existing")}
            >
              <FormControlLabel
                value="new"
                control={<Radio />}
                label={t("discovery.addDialog.createNewSet")}
              />
              <FormControlLabel
                value="existing"
                control={<Radio />}
                label={t("discovery.addDialog.addToExisting")}
              />
            </RadioGroup>
          </Box>
        )}

        {/* New set name input */}
        {mode === "new" && (
          <B4TextField
            label={t("discovery.addDialog.setName")}
            value={name}
            onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
              setName(e.target.value)
            }
            fullWidth
          />
        )}

        {/* Similar sets list */}
        {mode === "existing" && similarSets.length > 0 && (
          <Box>
            <Typography
              variant="subtitle2"
              sx={{ mb: 1, color: colors.text.secondary }}
            >
              {t("discovery.addDialog.similarSets")}
            </Typography>
            <Stack spacing={1}>
              {similarSets.map((set) => (
                <Box
                  key={set.id}
                  onClick={() => setSelectedSetId(set.id)}
                  sx={{
                    p: 2,
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
                      ` ${t("discovery.addDialog.nMore", { count: set.domains.length - 3 })}`}
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
