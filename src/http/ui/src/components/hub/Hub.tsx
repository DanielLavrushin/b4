import { useCallback, useEffect, useState } from "react";
import {
  Box,
  Button,
  CircularProgress,
  IconButton,
  InputAdornment,
  Stack,
  Typography,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import { useNavigate, useSearchParams } from "react-router";
import { B4Alert, B4Dialog, B4Section, B4TextField } from "@b4.elements";
import { ClearIcon, CommunityIcon, SearchIcon, WarningIcon } from "@b4.icons";
import { colors } from "@design";
import { ApiError } from "@api/apiClient";
import { SnackbarAction, useSnackbar } from "@context/SnackbarProvider";
import {
  useHubApply,
  useHubSet,
  useHubSets,
  useHubStatus,
  useHubSync,
  useHubUndoApply,
  useHubVote,
  useLocalSetsEnabled,
} from "@hooks/useHub";
import { CoveredSet, DomainReassignment } from "@models/sets";
import { HubApplyResponse, HubSet, HubVoteKind } from "@models/hub";
import { describeApiError, describeHubError } from "@utils";
import { CoveredDialog, CoveredReport } from "./CoveredDialog";
import { DetailsDialog } from "./DetailsDialog";
import { HubSetCard } from "./HubSetCard";
import { ReportDialog } from "./ReportDialog";
import { StatusPanel } from "./StatusPanel";
import { TestDialog } from "./TestDialog";
import { defaultTestDomain, warningText } from "./text";

const SEARCH_DEBOUNCE_MS = 400;
const PAGE_LIMIT = 50;

const normalizeDomain = (raw: string): string =>
  raw
    .trim()
    .toLowerCase()
    .replace(/^https?:\/\//, "")
    .split("/")[0];

export const HubBrowser = () => {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [searchParams, setSearchParams] = useSearchParams();
  const { showSuccess, showError, showSnackbar } = useSnackbar();

  const [query, setQuery] = useState(searchParams.get("domain") ?? "");
  const [domain, setDomain] = useState(normalizeDomain(query));
  const [testSet, setTestSet] = useState<HubSet | null>(null);
  const [reportSet, setReportSet] = useState<HubSet | null>(null);
  const [updateSet, setUpdateSet] = useState<HubSet | null>(null);
  const [report, setReport] = useState<CoveredReport | null>(null);

  useEffect(() => {
    const timer = setTimeout(
      () => setDomain(normalizeDomain(query)),
      SEARCH_DEBOUNCE_MS,
    );
    return () => clearTimeout(timer);
  }, [query]);

  const status = useHubStatus();
  const ready = Boolean(status.data?.enabled && status.data?.configured);
  const sets = useHubSets(domain, PAGE_LIMIT, ready);
  const sync = useHubSync();
  const apply = useHubApply();
  const undo = useHubUndoApply();
  const enable = useLocalSetsEnabled();
  const vote = useHubVote();

  const detailsId = searchParams.get("set");
  const details = useHubSet(ready ? detailsId : null);

  const openDetails = useCallback(
    (id: string | null) => {
      const next = new URLSearchParams(searchParams);
      if (id) next.set("set", id);
      else next.delete("set");
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );

  const describeMoved = (moved?: DomainReassignment[]): string => {
    if (!moved || moved.length === 0) return "";
    return t("hub.apply.moved", {
      domains: [...new Set(moved.map((m) => m.domain))].join(", "),
      sets: [...new Set(moved.map((m) => m.set_name))].join(", "),
    });
  };

  const undoApply = async (localSetId: string): Promise<boolean> => {
    try {
      await undo.mutateAsync(localSetId);
      showSuccess(t("hub.apply.undone"));
      return true;
    } catch (e) {
      showError(t("hub.apply.undoFailed", { error: describeApiError(e) }));
      return false;
    }
  };

  const applyFailure = (e: unknown): string => {
    const code = e instanceof ApiError ? e.code : undefined;
    return code === "hub_unreachable" ||
      (e instanceof ApiError && e.status === 502)
      ? t("hub.apply.payloadUnreachable")
      : t("hub.apply.failed", { error: describeApiError(e) });
  };

  const announce = (
    set: HubSet,
    res: HubApplyResponse,
    message: string,
    undoable: boolean,
  ) => {
    const covered = res.covered ?? [];
    const actions: SnackbarAction[] = [];
    if (covered.length === 0) {
      actions.push({
        label: t("hub.apply.openSet"),
        onClick: () => {
          void navigate(`/sets/${res.id}`);
        },
      });
      if (undoable) {
        actions.push({
          label: t("hub.apply.undo"),
          onClick: () => {
            void undoApply(res.id);
          },
        });
      }
    }
    showSuccess(
      [message, describeMoved(res.moved)].filter(Boolean).join(" "),
      actions,
    );
    if (res.warnings?.length) {
      showSnackbar(
        res.warnings
          .map((w) => warningText(t, "sets.importExport.warnings", w))
          .join(" "),
        "warning",
      );
    }
    setReport(
      covered.length > 0
        ? {
            set,
            localId: res.id,
            name: res.name,
            covered,
            undoable,
            disabled: [],
          }
        : null,
    );
  };

  const handleUpdate = async (set: HubSet) => {
    const applied = set.applied;
    setUpdateSet(null);
    if (!applied) return;
    try {
      const res = await apply.mutateAsync({ set, replace: applied.set_id });
      announce(
        set,
        res,
        t("hub.apply.updated", { name: res.name, version: set.version }),
        false,
      );
    } catch (e) {
      showError(applyFailure(e));
    }
  };

  const handleApply = async (set: HubSet) => {
    try {
      const res = await apply.mutateAsync({ set });
      announce(set, res, t("hub.apply.done", { name: res.name }), true);
    } catch (e) {
      showError(applyFailure(e));
    }
  };

  const handleCoveredDisable = async (covered: CoveredSet) => {
    try {
      await enable.mutateAsync({ ids: [covered.set_id], enabled: false });
      setReport(
        (current) =>
          current && {
            ...current,
            disabled: [...current.disabled, covered.set_id],
          },
      );
      showSuccess(t("hub.apply.covered.disabled", { name: covered.set_name }));
    } catch (e) {
      showError(
        t("hub.apply.covered.disableFailed", { error: describeApiError(e) }),
      );
    }
  };

  const handleCoveredReplace = async (covered: CoveredSet) => {
    if (!report) return;
    const { set, localId } = report;
    try {
      const res = await apply.mutateAsync({
        set,
        replace: covered.set_id,
        discard: localId,
      });
      announce(
        set,
        res,
        t("hub.apply.covered.replaced", { name: covered.set_name }),
        false,
      );
    } catch (e) {
      showError(applyFailure(e));
    }
  };

  const handleCoveredUndo = async () => {
    if (!report || !(await undoApply(report.localId))) return;
    setReport(null);
    if (report.disabled.length === 0) return;
    try {
      await enable.mutateAsync({ ids: report.disabled, enabled: true });
    } catch (e) {
      showError(
        t("hub.apply.covered.enableFailed", { error: describeApiError(e) }),
      );
    }
  };

  const handleVote = async (set: HubSet, kind: HubVoteKind) => {
    try {
      const res = await vote.mutateAsync({
        id: set.id,
        kind,
        domain: set.match ? domain : undefined,
      });
      showSuccess(res.sent ? t("hub.vote.sent") : t("hub.vote.queued"));
    } catch (e) {
      const code = e instanceof ApiError ? e.code : undefined;
      if (code === "not_applied") showError(t("hub.vote.notApplied"));
      else if (code === "modified") showError(t("hub.vote.modified"));
      else showError(t("hub.vote.failed", { error: describeHubError(e, t) }));
    }
  };

  const handleSync = () => {
    sync.mutate(undefined, {
      onSuccess: () => showSuccess(t("hub.status.synced")),
      onError: (e) =>
        showError(t("hub.status.syncFailed", { error: describeApiError(e) })),
    });
  };

  const busy =
    apply.isPending || undo.isPending || vote.isPending || enable.isPending;
  const list = sets.data?.sets ?? [];
  const total = sets.data?.total ?? list.length;

  let resultsLine = "";
  if (sets.data) {
    if (domain) {
      resultsLine =
        total > 0
          ? t("hub.search.results", { count: total, domain })
          : t("hub.search.noResults", { domain });
    } else {
      resultsLine =
        total > 0
          ? t("hub.search.all", { count: total })
          : t("hub.search.empty");
    }
  }

  return (
    <Stack spacing={3}>
      <B4Section
        title={t("hub.title")}
        description={t("hub.description")}
        icon={<CommunityIcon />}
      >
        <StatusPanel
          status={status.data}
          loading={status.isLoading}
          error={status.error}
          syncing={sync.isPending}
          onSync={handleSync}
        />
        {ready && (
          <B4TextField
            label={t("hub.search.label")}
            placeholder={t("hub.search.placeholder")}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            slotProps={{
              input: {
                startAdornment: (
                  <InputAdornment position="start">
                    <SearchIcon fontSize="small" />
                  </InputAdornment>
                ),
                endAdornment: query ? (
                  <InputAdornment position="end">
                    <IconButton
                      size="small"
                      onClick={() => setQuery("")}
                      aria-label={t("hub.search.clear")}
                    >
                      <ClearIcon fontSize="small" />
                    </IconButton>
                  </InputAdornment>
                ) : undefined,
              },
            }}
          />
        )}
      </B4Section>

      {ready && (
        <Stack spacing={1.5}>
          <Stack direction="row" alignItems="center" spacing={1.5}>
            <Typography variant="body2" sx={{ color: colors.text.secondary }}>
              {resultsLine}
            </Typography>
            {sets.isFetching && (
              <CircularProgress size={14} sx={{ color: colors.secondary }} />
            )}
          </Stack>
          {sets.isError && (
            <B4Alert severity="error">
              {t("hub.search.failed", { error: describeApiError(sets.error) })}
            </B4Alert>
          )}
          {list.map((set) => (
            <HubSetCard
              key={`${set.id}:${set.version}`}
              set={set}
              busy={busy}
              onApply={(s) => void handleApply(s)}
              onUpdate={setUpdateSet}
              onDetails={(s) => openDetails(s.id)}
              onVote={(s, kind) => void handleVote(s, kind)}
              onTest={setTestSet}
              onReport={setReportSet}
              onOpenLocal={(localId) => {
                void navigate(`/sets/${localId}`);
              }}
            />
          ))}
          {sets.isLoading && (
            <Box sx={{ display: "flex", justifyContent: "center", py: 4 }}>
              <CircularProgress sx={{ color: colors.secondary }} />
            </Box>
          )}
        </Stack>
      )}

      <DetailsDialog
        open={Boolean(detailsId) && ready}
        set={details.data ?? null}
        loading={details.isLoading}
        error={details.error}
        busy={busy}
        onApply={(s) => void handleApply(s)}
        onUpdate={setUpdateSet}
        onOpenLocal={(localId) => {
          void navigate(`/sets/${localId}`);
        }}
        onReport={setReportSet}
        onClose={() => openDetails(null)}
      />

      <B4Dialog
        open={Boolean(updateSet)}
        title={t("hub.apply.confirmTitle")}
        subtitle={updateSet?.title}
        icon={<WarningIcon />}
        onClose={() => setUpdateSet(null)}
        actions={
          <>
            <Button onClick={() => setUpdateSet(null)}>
              {t("core.cancel")}
            </Button>
            <Box sx={{ flex: 1 }} />
            <Button
              variant="contained"
              disabled={busy}
              onClick={() => {
                if (updateSet) void handleUpdate(updateSet);
              }}
            >
              {t("hub.apply.replace")}
            </Button>
          </>
        }
      >
        <Typography sx={{ mt: 2 }}>
          {t("hub.apply.confirmReplace", {
            name: updateSet?.applied?.set_name ?? "",
            version: updateSet?.version ?? 0,
          })}
        </Typography>
      </B4Dialog>

      <CoveredDialog
        report={report}
        busy={busy}
        onDisable={(c) => void handleCoveredDisable(c)}
        onReplace={(c) => void handleCoveredReplace(c)}
        onUndo={() => void handleCoveredUndo()}
        onOpenSet={() => {
          if (report) void navigate(`/sets/${report.localId}`);
        }}
        onClose={() => setReport(null)}
      />

      {testSet && (
        <TestDialog
          key={testSet.id}
          set={testSet}
          defaultDomain={defaultTestDomain(testSet, domain)}
          onClose={() => setTestSet(null)}
        />
      )}

      {reportSet && (
        <ReportDialog
          key={reportSet.id}
          set={reportSet}
          onClose={() => setReportSet(null)}
        />
      )}
    </Stack>
  );
};
