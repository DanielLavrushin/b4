import { useEffect, useMemo, useRef, useState } from "react";
import {
  Box,
  Button,
  CircularProgress,
  IconButton,
  Stack,
  Tooltip,
  Typography,
} from "@mui/material";
import {
  AddIcon,
  AsnIcon,
  ClearIcon,
  EyeIcon,
  InfoIcon,
  RefreshIcon,
} from "@b4.icons";
import {
  B4Alert,
  B4Badge,
  B4Hint,
  B4PlusButton,
  B4TextField,
} from "@b4.elements";
import { colors, radius, typography } from "@design";
import { useTranslation } from "react-i18next";
import { ApiError } from "@api/apiClient";
import { asnApi } from "@api/asn";
import { useSnackbar } from "@context/SnackbarProvider";
import { useAsnCache, useAsnViews } from "@hooks/useAsn";
import { AsnFacts, AsnLargeNetworkAlert } from "@common/AsnFacts";
import {
  AsnLookup,
  AsnView,
  formatAsn,
  isAsnResolved,
  parseAsnInput,
} from "@models/asn";
import { B4SetConfig } from "@models/config";
import { localizeApiError } from "@utils";
import { SetStats } from "../Manager";
import { OtherSetsTargets, asnOverlapKey } from "./overlap";
import { AsnPrefixesDialog } from "./AsnPrefixesDialog";

interface AsnTabProps {
  config: B4SetConfig;
  stats?: SetStats;
  otherSetsTargets?: OtherSetsTargets;
  onChange: (field: string, value: string | string[] | boolean) => void;
}

interface ResolveFailure {
  id: string;
  message: string;
  retryable: boolean;
}

type RowState = "loading" | "resolved" | "unresolved" | "unknown";

export const AsnTab = ({
  config,
  stats,
  otherSetsTargets,
  onChange,
}: AsnTabProps) => {
  const { t, i18n } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const asns = useMemo(() => config.targets.asns ?? [], [config.targets.asns]);
  const asnsRef = useRef(asns);
  useEffect(() => {
    asnsRef.current = asns;
  }, [asns]);

  const viewsQuery = useAsnViews({ watchIds: asns });
  const views = viewsQuery.data;
  const { store, invalidate } = useAsnCache();

  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [inputError, setInputError] = useState("");
  const [notice, setNotice] = useState("");
  const [failure, setFailure] = useState<ResolveFailure | null>(null);
  const [lookup, setLookup] = useState<AsnLookup | null>(null);
  const [refreshing, setRefreshing] = useState<Set<string>>(new Set());
  const [previewId, setPreviewId] = useState<string | null>(null);

  const parsed = input.trim() ? parseAsnInput(input) : null;
  const overlapSets =
    parsed?.kind === "asn"
      ? otherSetsTargets?.get(asnOverlapKey(parsed.id))
      : undefined;

  const clearFeedback = () => {
    setInputError("");
    setNotice("");
    setFailure(null);
    setLookup(null);
  };

  const handleInput = (value: string) => {
    setInput(value);
    clearFeedback();
  };

  const addIds = (ids: string[]) => {
    const current = asnsRef.current;
    const next = [...current];
    for (const id of ids) if (!next.includes(id)) next.push(id);
    if (next.length !== current.length) onChange("targets.asns", next);
  };

  const resolveAndAdd = async (id: string) => {
    if (asnsRef.current.includes(id)) {
      setNotice(t("sets.targets.asn.alreadyAdded", { asn: formatAsn(id) }));
      return;
    }
    setBusy(true);
    setFailure(null);
    setInputError("");
    try {
      const view = await asnApi.resolve(id);
      if (views) store(view);
      else void invalidate();
      addIds([id]);
      setInput("");
      setLookup(null);
      setNotice("");
    } catch (e) {
      setFailure({
        id,
        message: localizeApiError(e),
        retryable: !(e instanceof ApiError && e.code === "asn_invalid"),
      });
    } finally {
      setBusy(false);
    }
  };

  const lookupAddress = async (ip: string) => {
    setBusy(true);
    setLookup(null);
    try {
      setLookup(await asnApi.lookup(ip));
    } catch (e) {
      setInputError(localizeApiError(e));
    } finally {
      setBusy(false);
    }
  };

  const handleSubmit = () => {
    if (!parsed || busy) return;
    clearFeedback();
    switch (parsed.kind) {
      case "asn":
        void resolveAndAdd(parsed.id);
        return;
      case "ip":
        void lookupAddress(parsed.ip);
        return;
      case "reserved":
        setInputError(t("sets.targets.asn.reserved", { value: parsed.value }));
        return;
      default:
        setInputError(t("sets.targets.asn.invalid", { value: parsed.value }));
    }
  };

  const addAnyway = () => {
    if (!failure) return;
    addIds([failure.id]);
    setFailure(null);
    setInput("");
    setLookup(null);
  };

  const refresh = async (id: string) => {
    setRefreshing((prev) => new Set(prev).add(id));
    try {
      const view = await asnApi.resolve(id, true);
      store(view);
      showSuccess(t("sets.targets.asn.refreshed", { asn: formatAsn(id) }));
    } catch (e) {
      showError(
        t("sets.targets.asn.refreshFailed", {
          asn: formatAsn(id),
          error: localizeApiError(e),
        }),
      );
      void invalidate();
    } finally {
      setRefreshing((prev) => {
        const next = new Set(prev);
        next.delete(id);
        return next;
      });
    }
  };

  const remove = (id: string) =>
    onChange(
      "targets.asns",
      asnsRef.current.filter((a) => a !== id),
    );

  const rowState = (id: string): RowState => {
    if (views) return isAsnResolved(views[id]) ? "resolved" : "unresolved";
    if (viewsQuery.isLoading) return "loading";
    if (stats?.asn_unresolved?.includes(id)) return "unresolved";
    return "unknown";
  };

  const previewView = previewId ? (views?.[previewId] ?? null) : null;

  return (
    <>
      <B4Hint>{t("sets.targets.asn.hint")}</B4Hint>

      <Box sx={{ mt: 3, maxWidth: 640 }}>
        <Typography
          variant="h6"
          sx={{ display: "flex", alignItems: "center", gap: 1, mb: 2 }}
        >
          <AsnIcon /> {t("sets.targets.asn.title")}
          <Tooltip title={t("sets.targets.asn.tooltip")}>
            <InfoIcon fontSize="small" color="action" />
          </Tooltip>
        </Typography>

        <Box sx={{ display: "flex", gap: 1, alignItems: "flex-start" }}>
          <B4TextField
            label={t("sets.targets.asn.inputLabel")}
            value={input}
            slotProps={{ htmlInput: { readOnly: busy } }}
            onChange={(e) => handleInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                handleSubmit();
              }
            }}
            helperText={t("sets.targets.asn.inputHelper")}
            placeholder={t("sets.targets.asn.inputPlaceholder")}
          />
          {busy ? (
            <Box sx={{ p: 1, display: "flex" }}>
              <CircularProgress size={24} sx={{ color: colors.secondary }} />
            </Box>
          ) : (
            <B4PlusButton onClick={handleSubmit} disabled={!parsed} />
          )}
        </Box>

        {overlapSets && overlapSets.length > 0 && (
          <B4Alert severity="info" sx={{ mt: 1 }}>
            {t("sets.targets.asn.otherSets", {
              asn: parsed?.kind === "asn" ? formatAsn(parsed.id) : "",
              sets: overlapSets.join(", "),
            })}
          </B4Alert>
        )}
        {notice && (
          <B4Alert severity="info" sx={{ mt: 1 }}>
            {notice}
          </B4Alert>
        )}
        {inputError && (
          <B4Alert severity="error" sx={{ mt: 1 }}>
            {inputError}
          </B4Alert>
        )}
        {failure && (
          <B4Alert
            severity="error"
            sx={{ mt: 1 }}
            action={
              failure.retryable ? (
                <Button color="inherit" size="small" onClick={addAnyway}>
                  {t("sets.targets.asn.addAnyway")}
                </Button>
              ) : undefined
            }
          >
            {t("sets.targets.asn.resolveFailed", {
              asn: formatAsn(failure.id),
              error: failure.message,
            })}
            {failure.retryable && (
              <Typography
                variant="caption"
                component="div"
                sx={{ mt: 0.5, opacity: 0.85 }}
              >
                {t("sets.targets.asn.addAnywayHint")}
              </Typography>
            )}
          </B4Alert>
        )}
        {lookup && (
          <LookupResult
            lookup={lookup}
            busy={busy}
            inSet={(id) => asns.includes(id)}
            onAdd={(id) => void resolveAndAdd(id)}
          />
        )}
      </Box>

      <Box sx={{ mt: 3 }}>
        <Box
          sx={{
            display: "flex",
            justifyContent: "space-between",
            alignItems: "center",
            gap: 1,
            mb: 1,
            flexWrap: "wrap",
          }}
        >
          <Box>
            <Typography variant="subtitle2">
              {t("sets.targets.asn.activeTitle")}
            </Typography>
            {!!stats?.asn_ips && (
              <Typography
                variant="caption"
                sx={{ color: colors.text.secondary }}
              >
                {t("sets.targets.asn.savedTotal", {
                  value: stats.asn_ips.toLocaleString(i18n.language),
                })}
              </Typography>
            )}
          </Box>
          {asns.length > 0 && (
            <Button
              size="small"
              onClick={() => onChange("targets.asns", [])}
              startIcon={<ClearIcon />}
            >
              {t("core.clearAll")}
            </Button>
          )}
        </Box>

        {viewsQuery.isError && asns.length > 0 && (
          <B4Alert severity="warning" sx={{ mb: 1 }}>
            {t("sets.targets.asn.loadFailed", {
              error: localizeApiError(viewsQuery.error),
            })}
          </B4Alert>
        )}

        {asns.length === 0 ? (
          <Typography
            variant="body2"
            sx={{ color: colors.text.secondary, fontStyle: "italic" }}
          >
            {t("sets.targets.asn.empty")}
          </Typography>
        ) : (
          <Stack spacing={1}>
            {asns.map((id) => (
              <AsnRow
                key={id}
                id={id}
                view={views?.[id]}
                state={rowState(id)}
                setName={config.name}
                ipVersion={config.targets.ip_version}
                refreshing={refreshing.has(id)}
                onRefresh={() => void refresh(id)}
                onPreview={() => setPreviewId(id)}
                onRemove={() => remove(id)}
              />
            ))}
          </Stack>
        )}
      </Box>

      <AsnPrefixesDialog view={previewView} onClose={() => setPreviewId(null)} />
    </>
  );
};

interface LookupResultProps {
  lookup: AsnLookup;
  busy: boolean;
  inSet: (id: string) => boolean;
  onAdd: (id: string) => void;
}

const LookupResult = ({ lookup, busy, inSet, onAdd }: LookupResultProps) => {
  const { t } = useTranslation();
  return (
    <Box
      sx={{
        mt: 1.5,
        p: 1.5,
        bgcolor: colors.background.paper,
        border: `1px solid ${colors.border.default}`,
        borderRadius: radius.sm,
      }}
    >
      <Typography variant="subtitle2">
        {t("sets.targets.asn.lookupTitle", { ip: lookup.ip })}
      </Typography>
      {lookup.prefix && (
        <Typography
          sx={{
            ...typography.recipes.monoSmall,
            color: colors.text.secondary,
          }}
        >
          {t("sets.targets.asn.lookupPrefix", { prefix: lookup.prefix })}
        </Typography>
      )}
      {lookup.asns.length === 0 ? (
        <Typography variant="body2" sx={{ mt: 1, color: colors.text.secondary }}>
          {t("sets.targets.asn.lookupNone", { ip: lookup.ip })}
        </Typography>
      ) : (
        <Stack spacing={0.75} sx={{ mt: 1 }}>
          {lookup.asns.map((origin) => {
            const present = inSet(origin.id);
            return (
              <Stack
                key={origin.id}
                direction="row"
                alignItems="center"
                justifyContent="space-between"
                gap={1}
              >
                <Box sx={{ minWidth: 0 }}>
                  <Typography
                    component="span"
                    sx={{
                      ...typography.recipes.monoSmall,
                      fontWeight: typography.weights.bold,
                      color: colors.secondary,
                      mr: 1,
                    }}
                  >
                    {formatAsn(origin.id)}
                  </Typography>
                  <Typography component="span" variant="body2">
                    {origin.name || t("sets.targets.asn.unnamed")}
                  </Typography>
                </Box>
                <Button
                  size="small"
                  startIcon={<AddIcon />}
                  disabled={busy || present}
                  onClick={() => onAdd(origin.id)}
                  sx={{ flexShrink: 0 }}
                >
                  {present
                    ? t("sets.targets.asn.inSet")
                    : t("sets.targets.asn.lookupAdd", {
                        asn: formatAsn(origin.id),
                      })}
                </Button>
              </Stack>
            );
          })}
        </Stack>
      )}
    </Box>
  );
};

interface AsnRowProps {
  id: string;
  view?: AsnView;
  state: RowState;
  setName: string;
  ipVersion?: string;
  refreshing: boolean;
  onRefresh: () => void;
  onPreview: () => void;
  onRemove: () => void;
}

const AsnRow = ({
  id,
  view,
  state,
  setName,
  ipVersion,
  refreshing,
  onRefresh,
  onPreview,
  onRemove,
}: AsnRowProps) => {
  const { t, i18n } = useTranslation();
  const unresolved = state === "unresolved";
  const filtered =
    ipVersion === "4"
      ? view?.v4_count
      : ipVersion === "6"
        ? view?.v6_count
        : undefined;
  const accent = unresolved ? colors.state.warning : colors.border.default;
  const otherSets = (view?.used_by ?? []).filter((name) => name !== setName);
  const showFiltered =
    state === "resolved" &&
    filtered !== undefined &&
    !!view &&
    filtered !== view.prefix_count;

  return (
    <Box
      sx={{
        p: 1.5,
        bgcolor: colors.background.paper,
        border: `1px solid ${accent}`,
        borderLeft: `3px solid ${unresolved ? colors.state.warning : colors.secondary}`,
        borderRadius: radius.sm,
      }}
    >
      <Stack direction="row" alignItems="flex-start" gap={1}>
        <Box sx={{ flex: 1, minWidth: 0 }}>
          <Stack direction="row" alignItems="center" gap={1} flexWrap="wrap">
            <Typography
              sx={{
                ...typography.recipes.monoSmall,
                fontSize: typography.sizes.md,
                fontWeight: typography.weights.bold,
                color: unresolved ? colors.state.warning : colors.secondary,
              }}
            >
              {formatAsn(id)}
            </Typography>
            <Typography
              variant="body2"
              noWrap
              sx={{ color: colors.text.primary, minWidth: 0 }}
            >
              {view?.name || (state === "loading" ? "" : t("sets.targets.asn.unnamed"))}
            </Typography>
            {unresolved && (
              <B4Badge
                label={t("core.asn.pending")}
                color="warning"
                variant="outlined"
              />
            )}
          </Stack>

          {state === "loading" && (
            <Typography variant="caption" sx={{ color: colors.text.secondary }}>
              {t("core.loading")}
            </Typography>
          )}
          {state === "resolved" && view && <AsnFacts view={view} sx={{ mt: 0.5 }} />}
          {showFiltered && (
            <Typography
              variant="caption"
              component="div"
              sx={{ color: colors.text.secondary }}
            >
              {t("sets.targets.asn.filtered", {
                value: (filtered ?? 0).toLocaleString(i18n.language),
                version: ipVersion,
              })}
            </Typography>
          )}
          {unresolved && (
            <Typography
              variant="caption"
              component="div"
              sx={{ mt: 0.5, color: colors.state.warning }}
            >
              {t("core.asn.unresolved")}
            </Typography>
          )}
          {view?.last_error && (
            <Typography
              variant="caption"
              component="div"
              sx={{
                mt: 0.5,
                color: unresolved ? colors.state.warning : colors.text.secondary,
                overflowWrap: "anywhere",
              }}
            >
              {t("core.asn.lastError", { error: view.last_error })}
            </Typography>
          )}
          {otherSets.length > 0 && (
            <Typography
              variant="caption"
              component="div"
              sx={{ mt: 0.5, color: colors.text.secondary }}
            >
              {t("sets.targets.asn.usedByOthers", { sets: otherSets.join(", ") })}
            </Typography>
          )}
        </Box>

        <Stack direction="row" alignItems="center" sx={{ flexShrink: 0 }}>
          <Tooltip title={t("sets.targets.asn.refresh")}>
            <span>
              <IconButton
                size="small"
                disabled={refreshing}
                onClick={onRefresh}
                sx={{ "&:hover": { color: colors.secondary } }}
              >
                {refreshing ? <CircularProgress size={18} /> : <RefreshIcon fontSize="small" />}
              </IconButton>
            </span>
          </Tooltip>
          <Tooltip title={t("sets.targets.asn.showPrefixes")}>
            <span>
              <IconButton
                size="small"
                disabled={!view || view.prefixes.length === 0}
                onClick={onPreview}
                sx={{ "&:hover": { color: colors.secondary } }}
              >
                <EyeIcon fontSize="small" />
              </IconButton>
            </span>
          </Tooltip>
          <Tooltip title={t("sets.targets.asn.remove")}>
            <IconButton
              size="small"
              onClick={onRemove}
              sx={{ "&:hover": { color: colors.secondary } }}
            >
              <ClearIcon fontSize="small" />
            </IconButton>
          </Tooltip>
        </Stack>
      </Stack>

      {state === "resolved" && <AsnLargeNetworkAlert view={view} sx={{ mt: 1 }} />}
    </Box>
  );
};
