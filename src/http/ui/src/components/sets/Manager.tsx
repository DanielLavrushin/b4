import {
  Box,
  Button,
  FormControlLabel,
  Grid,
  InputAdornment,
  List,
  ListItem,
  ListItemText,
  Paper,
  Stack,
  Switch,
  TextField,
  Tooltip,
  Typography,
} from "@mui/material";
import { useCallback, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router";

import {
  AddIcon,
  CheckIcon,
  ClearIcon,
  CompareIcon,
  DomainIcon,
  SetsIcon,
  WarningIcon,
} from "@b4.icons";
import SearchOutlinedIcon from "@mui/icons-material/SearchOutlined";

import {
  DndContext,
  DragEndEvent,
  DragOverlay,
  DragStartEvent,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  SortableContext,
  rectSortingStrategy,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

import { B4Dialog, B4Section } from "@b4.elements";
import { useSnackbar } from "@context/SnackbarProvider";
import { reportSaveError } from "@utils";

import { SetCompare } from "./Compare";
import { SetCard } from "./SetCard";

import { colors, radius } from "@design";
import { useSets } from "@hooks/useSets";
import { B4Config, B4SetConfig } from "@models/config";
import { useTranslation } from "react-i18next";

export interface SetStats {
  manual_domains: number;
  manual_ips: number;
  geosite_domains: number;
  geoip_ips: number;
  total_domains: number;
  total_ips: number;
  geosite_category_breakdown?: Record<string, number>;
  geoip_category_breakdown?: Record<string, number>;
}

export interface SetWithStats extends B4SetConfig {
  stats: SetStats;
}

interface SetsManagerProps {
  config: B4Config & { sets?: SetWithStats[] };
  onRefresh: () => void;
}

interface SortableCardWrapperProps {
  id: string;
  outerRef?: (el: HTMLDivElement | null) => void;
  children:
    | React.ReactNode
    | ((props: React.HTMLAttributes<HTMLDivElement>) => React.JSX.Element);
}

const SortableCardWrapper = ({
  id,
  outerRef,
  children,
}: SortableCardWrapperProps) => {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id });

  const combinedRef = (el: HTMLDivElement | null) => {
    setNodeRef(el);
    outerRef?.(el);
  };

  return (
    <Box
      ref={combinedRef}
      style={{
        height: "100%",
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.4 : 1,
        zIndex: isDragging ? 1 : 0,
      }}
    >
      {typeof children === "function"
        ? children({ ...attributes, ...listeners })
        : children}
    </Box>
  );
};

export const SetsManager = ({ config, onRefresh }: SetsManagerProps) => {
  const { t } = useTranslation();
  const { showSuccess, showError } = useSnackbar();
  const navigate = useNavigate();
  const {
    deleteSet,
    deleteSets,
    duplicateSet,
    reorderSets,
    updateSet,
    setEnabledForSets,
  } = useSets();

  const [filterText, setFilterText] = useState("");
  const [selectionMode, setSelectionMode] = useState(false);
  const [selectedIds, setSelectedIds] = useState<Set<string>>(new Set());
  const [deleteDialog, setDeleteDialog] = useState<{
    open: boolean;
    setId: string | null;
  }>({
    open: false,
    setId: null,
  });
  const [batchDeleteDialog, setBatchDeleteDialog] = useState(false);
  const [compareDialog, setCompareDialog] = useState<{
    open: boolean;
    setA: B4SetConfig | null;
    setB: B4SetConfig | null;
  }>({ open: false, setA: null, setB: null });

  const [activeId, setActiveId] = useState<string | null>(null);
  const [highlightedSetId, setHighlightedSetId] = useState<string | null>(null);
  const cardRefs = useRef(new Map<string, HTMLDivElement>());

  const setsData = config.sets || [];
  const sets = setsData.map((s) => ("set" in s ? s.set : s)) as B4SetConfig[];
  const setsStats = setsData.map((s) =>
    "stats" in s ? s.stats : null,
  ) as (SetStats | null)[];

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 8 },
    }),
  );

  const summaryStats = useMemo(() => {
    const enabledCount = sets.filter((s) => s.enabled).length;
    const totalDomains = setsStats.reduce(
      (acc, s) => acc + (s?.total_domains || 0),
      0,
    );
    const totalIps = setsStats.reduce((acc, s) => acc + (s?.total_ips || 0), 0);
    return {
      total: sets.length,
      enabled: enabledCount,
      totalDomains,
      totalIps,
    };
  }, [sets, setsStats]);

  const escalationMaps = useMemo(() => {
    const byId = new Map<string, B4SetConfig>();
    for (const s of sets) byId.set(s.id, s);

    const escalatesTo = new Map<string, { id: string; name: string }>();
    const escalatedFrom = new Map<string, { id: string; name: string }[]>();

    for (const s of sets) {
      const targetId = s.escalate?.to;
      if (!targetId) continue;
      const target = byId.get(targetId);
      if (!target) continue;
      escalatesTo.set(s.id, { id: target.id, name: target.name || target.id });
      const list = escalatedFrom.get(target.id) ?? [];
      list.push({ id: s.id, name: s.name || s.id });
      escalatedFrom.set(target.id, list);
    }
    return { escalatesTo, escalatedFrom };
  }, [sets]);

  const handleEscalationHover = useCallback((setId: string | null) => {
    setHighlightedSetId(setId);
    if (setId) {
      const el = cardRefs.current.get(setId);
      el?.scrollIntoView({ behavior: "smooth", block: "nearest" });
    }
  }, []);

  const handleEscalationClick = useCallback(
    (setId: string) => {
      navigate(`/sets/${setId}`)?.catch(() => {});
    },
    [navigate],
  );

  const registerCardRef = useCallback(
    (id: string) => (el: HTMLDivElement | null) => {
      if (el) cardRefs.current.set(id, el);
      else cardRefs.current.delete(id);
    },
    [],
  );

  const filteredSets = useMemo(() => {
    if (!filterText.trim()) return sets;
    const lower = filterText.toLowerCase();
    return sets.filter((set) => {
      if (set.name.toLowerCase().includes(lower)) return true;
      if (
        set.targets?.sni_domains?.some((d) => d.toLowerCase().includes(lower))
      )
        return true;
      if (
        set.targets?.geosite_categories?.some((c) =>
          c.toLowerCase().includes(lower),
        )
      )
        return true;
      return false;
    });
  }, [sets, filterText]);

  const handleDragStart = (event: DragStartEvent) => {
    setActiveId(event.active.id as string);
  };

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    setActiveId(null);

    if (!over || active.id === over.id) return;

    const oldIndex = sets.findIndex((s) => s.id === active.id);
    const newIndex = sets.findIndex((s) => s.id === over.id);

    if (oldIndex === -1 || newIndex === -1) return;

    const newOrder = [...sets];
    const [removed] = newOrder.splice(oldIndex, 1);
    newOrder.splice(newIndex, 0, removed);

    void (async () => {
      const result = await reorderSets(newOrder.map((s) => s.id));
      if (result.success) onRefresh();
    })();
  };

  const activeSet = activeId ? sets.find((s) => s.id === activeId) : null;

  const handleAddSet = () => {
    navigate("/sets/new")?.catch(() => {});
  };

  const handleEditSet = (set: B4SetConfig) => {
    navigate(`/sets/${set.id}`)?.catch(() => {});
  };

  const handleDeleteSet = () => {
    const { setId } = deleteDialog;
    if (!setId) return;
    void (async () => {
      const result = await deleteSet(setId);
      if (result.success) {
        showSuccess(t("sets.manager.setDeleted"));
        setDeleteDialog({ open: false, setId: null });
        onRefresh();
      } else {
        reportSaveError(result.error, showError, t, "sets.manager.failedToDelete");
      }
    })();
  };

  const handleDuplicateSet = (set: B4SetConfig) => {
    void (async () => {
      const result = await duplicateSet(set);
      if (result.success) {
        showSuccess(t("sets.manager.setDuplicated"));
        onRefresh();
      } else {
        reportSaveError(result.error, showError, t, "sets.manager.failedToDuplicate");
      }
    })();
  };

  const handleToggleSelection = (id: string) => {
    setSelectedIds((prev) => {
      const next = new Set(prev);
      if (next.has(id)) {
        next.delete(id);
      } else {
        next.add(id);
      }
      return next;
    });
  };

  const handleSelectAll = () => {
    setSelectedIds(new Set(filteredSets.map((s) => s.id)));
  };

  const handleDeselectAll = () => {
    setSelectedIds(new Set());
  };

  const handleExitSelectionMode = () => {
    setSelectionMode(false);
    setSelectedIds(new Set());
  };

  const handleBatchDelete = () => {
    if (selectedIds.size === 0) return;
    void (async () => {
      const result = await deleteSets(Array.from(selectedIds));
      if (result.success) {
        showSuccess(t("sets.manager.setsDeleted", { count: selectedIds.size }));
        setBatchDeleteDialog(false);
        handleExitSelectionMode();
        onRefresh();
      } else {
        reportSaveError(result.error, showError, t, "sets.manager.failedToDeleteSets");
      }
    })();
  };

  const handleToggleAll = (enabled: boolean) => {
    if (sets.length === 0) return;
    void (async () => {
      const ids = sets.map((s) => s.id);
      const result = await setEnabledForSets(ids, enabled);
      if (result.success) {
        showSuccess(
          t(enabled ? "sets.manager.allEnabled" : "sets.manager.allDisabled"),
        );
        onRefresh();
      } else {
        reportSaveError(result.error, showError, t, "sets.manager.failedToToggleSets");
      }
    })();
  };

  const handleToggleEnabled = (set: B4SetConfig, enabled: boolean) => {
    void (async () => {
      const updatedSet = { ...set, enabled };
      const result = await updateSet(updatedSet);
      if (result.success) {
        onRefresh();
      } else {
        reportSaveError(result.error, showError, t, "sets.manager.failedToUpdate");
      }
    })();
  };

  return (
    <Stack spacing={3}>
      <B4Section
        title={t("sets.manager.title")}
        description={t("sets.manager.description")}
        icon={<SetsIcon />}
      >
        <Paper
          elevation={0}
          sx={{
            p: 2,
            mb: 3,
            bgcolor: colors.background.dark,
            border: `1px solid ${colors.border.default}`,
            borderRadius: radius.md,
          }}
        >
          <Stack
            direction="row"
            spacing={4}
            alignItems="center"
            justifyContent="space-between"
            flexWrap="wrap"
            useFlexGap
          >
            <Stack direction="row" spacing={4}>
              <StatItem
                value={summaryStats.total}
                label={t("sets.manager.totalSets")}
                color={colors.text.primary}
              />
              <StatItem
                value={summaryStats.enabled}
                label={t("sets.manager.enabled")}
                color={colors.tertiary}
                icon={<CheckIcon sx={{ fontSize: 16 }} />}
              />
              <StatItem
                value={summaryStats.totalDomains.toLocaleString()}
                label={t("core.domains")}
                color={colors.secondary}
                icon={<DomainIcon sx={{ fontSize: 16 }} />}
              />
            </Stack>

            <Stack direction="row" spacing={2} alignItems="center">
              {sets.length > 0 && !selectionMode && (
                <Tooltip title={t("sets.manager.toggleAllTooltip")}>
                  <FormControlLabel
                    control={
                      <Switch
                        size="small"
                        checked={summaryStats.enabled === summaryStats.total}
                        onChange={(_, checked) => handleToggleAll(checked)}
                      />
                    }
                    label={
                      <Typography
                        variant="body2"
                        sx={{
                          color: colors.text.secondary,
                          whiteSpace: "nowrap",
                          ml: 1,
                        }}
                      >
                        {summaryStats.enabled === summaryStats.total
                          ? t("sets.manager.disableAll")
                          : t("sets.manager.enableAll")}
                      </Typography>
                    }
                    sx={{ mr: 0 }}
                  />
                </Tooltip>
              )}
              <TextField
                size="small"
                placeholder={t("sets.manager.searchPlaceholder")}
                value={filterText}
                onChange={(e) => setFilterText(e.target.value)}
                slotProps={{
                  input: {
                    startAdornment: (
                      <InputAdornment position="start">
                        <SearchOutlinedIcon
                          sx={{ fontSize: 20, color: colors.text.secondary }}
                        />
                      </InputAdornment>
                    ),
                  },
                }}
                sx={{
                  width: 200,
                  "& .MuiOutlinedInput-root": {
                    bgcolor: colors.background.paper,
                  },
                }}
              />
              {selectionMode ? (
                <>
                  <Typography
                    variant="body2"
                    sx={{ color: colors.text.secondary, whiteSpace: "nowrap" }}
                  >
                    {selectedIds.size} {t("sets.manager.selected")}
                  </Typography>
                  <Button
                    size="small"
                    onClick={
                      selectedIds.size === filteredSets.length
                        ? handleDeselectAll
                        : handleSelectAll
                    }
                  >
                    {selectedIds.size === filteredSets.length
                      ? t("sets.manager.deselectAll")
                      : t("sets.manager.selectAll")}
                  </Button>
                  <Button
                    size="small"
                    variant="contained"
                    color="error"
                    startIcon={<ClearIcon />}
                    disabled={selectedIds.size === 0}
                    onClick={() => setBatchDeleteDialog(true)}
                  >
                    {t("sets.manager.deleteCount")} ({selectedIds.size})
                  </Button>
                  <Button size="small" onClick={handleExitSelectionMode}>
                    {t("core.cancel")}
                  </Button>
                </>
              ) : (
                <>
                  {sets.length > 0 && (
                    <Button
                      startIcon={<CheckIcon />}
                      onClick={() => setSelectionMode(true)}
                      variant="outlined"
                      size="small"
                    >
                      {t("sets.manager.select")}
                    </Button>
                  )}
                  <Button
                    startIcon={<AddIcon />}
                    onClick={handleAddSet}
                    variant="contained"
                  >
                    {t("sets.manager.createSet")}
                  </Button>
                </>
              )}
            </Stack>
          </Stack>
        </Paper>

        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragStart={handleDragStart}
          onDragEnd={handleDragEnd}
        >
          <SortableContext
            items={filteredSets.map((s) => s.id)}
            strategy={rectSortingStrategy}
          >
            <Grid container spacing={3}>
              {filteredSets.map((set) => {
                const index = sets.findIndex((s) => s.id === set.id);
                const stats = setsStats[index] || undefined;

                return (
                  <Grid key={set.id} size={{ xs: 12, sm: 6, lg: 4, xl: 3 }}>
                    <SortableCardWrapper
                      id={set.id}
                      outerRef={registerCardRef(set.id)}
                    >
                      {(
                        dragHandleProps: React.HTMLAttributes<HTMLDivElement>,
                      ) => (
                        <SetCard
                          set={set}
                          stats={stats}
                          index={index}
                          onEdit={() => handleEditSet(set)}
                          onDuplicate={() => handleDuplicateSet(set)}
                          onCompare={() =>
                            setCompareDialog({
                              open: true,
                              setA: set,
                              setB: null,
                            })
                          }
                          onDelete={() =>
                            setDeleteDialog({ open: true, setId: set.id })
                          }
                          onToggleEnabled={(enabled) =>
                            handleToggleEnabled(set, enabled)
                          }
                          dragHandleProps={dragHandleProps}
                          selectionMode={selectionMode}
                          selected={selectedIds.has(set.id)}
                          onSelect={() => handleToggleSelection(set.id)}
                          escalatesTo={escalationMaps.escalatesTo.get(set.id)}
                          escalatedFrom={escalationMaps.escalatedFrom.get(set.id)}
                          highlighted={highlightedSetId === set.id}
                          onEscalationHover={handleEscalationHover}
                          onEscalationClick={handleEscalationClick}
                        />
                      )}
                    </SortableCardWrapper>
                  </Grid>
                );
              })}
            </Grid>
          </SortableContext>

          <DragOverlay>
            {activeSet ? (
              <Box
                sx={{
                  p: 3,
                  bgcolor: colors.background.paper,
                  border: `2px solid ${colors.secondary}`,
                  borderRadius: radius.md,
                  boxShadow: `0 16px 48px ${colors.accent.primary}60`,
                  minWidth: 280,
                }}
              >
                <Typography variant="h6" fontWeight={600}>
                  {activeSet.name}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {activeSet.fragmentation.strategy.toUpperCase()}
                </Typography>
              </Box>
            ) : null}
          </DragOverlay>
        </DndContext>

        {sets.length === 0 && !filterText && (
          <Paper
            elevation={0}
            sx={{
              p: 6,
              textAlign: "center",
              border: `1px dashed ${colors.border.default}`,
              borderRadius: radius.md,
            }}
          >
            <SetsIcon
              sx={{ fontSize: 48, color: colors.text.secondary, mb: 2 }}
            />
            <Typography variant="h6" sx={{ mb: 1, color: colors.text.primary }}>
              {t("sets.manager.noSets")}
            </Typography>
            <Typography color="text.secondary" sx={{ mb: 3 }}>
              {t("sets.manager.noSetsHint")}
            </Typography>
            <Button
              startIcon={<AddIcon />}
              onClick={handleAddSet}
              variant="contained"
            >
              {t("sets.manager.createSet")}
            </Button>
          </Paper>
        )}

        {filteredSets.length === 0 && filterText && (
          <Paper
            elevation={0}
            sx={{
              p: 4,
              textAlign: "center",
              border: `1px dashed ${colors.border.default}`,
              borderRadius: radius.md,
            }}
          >
            <Typography color="text.secondary">
              {t("sets.manager.noMatch")} "{filterText}"
            </Typography>
          </Paper>
        )}
      </B4Section>

      <B4Dialog
        open={deleteDialog.open}
        title={t("sets.deleteDialog.title")}
        subtitle={t("sets.deleteDialog.subtitle")}
        icon={<WarningIcon />}
        onClose={() => setDeleteDialog({ open: false, setId: null })}
        actions={
          <>
            <Button
              onClick={() => setDeleteDialog({ open: false, setId: null })}
            >
              {t("core.cancel")}
            </Button>
            <Box sx={{ flex: 1 }} />
            <Button onClick={handleDeleteSet} variant="contained">
              {t("sets.deleteDialog.deleteSet")}
            </Button>
          </>
        }
      >
        <Typography sx={{ mt: 2 }}>
          {t("sets.deleteDialog.confirm")}{" "}
          <strong>{sets.find((s) => s.id === deleteDialog.setId)?.name}</strong>
          {"?"}
        </Typography>
      </B4Dialog>

      <B4Dialog
        open={batchDeleteDialog}
        title={`${t("sets.batchDeleteDialog.title")} (${selectedIds.size})`}
        subtitle={t("sets.batchDeleteDialog.subtitle")}
        icon={<WarningIcon />}
        onClose={() => setBatchDeleteDialog(false)}
        actions={
          <>
            <Button onClick={() => setBatchDeleteDialog(false)}>
              {t("core.cancel")}
            </Button>
            <Box sx={{ flex: 1 }} />
            <Button onClick={handleBatchDelete} variant="contained">
              {t("core.delete")} ({selectedIds.size})
            </Button>
          </>
        }
      >
        <Typography sx={{ mb: 1 }}>
          {t("sets.batchDeleteDialog.confirm")}
        </Typography>
        <Box
          component="ul"
          sx={{ m: 0, pl: 2, maxHeight: 200, overflow: "auto" }}
        >
          {sets
            .filter((s) => selectedIds.has(s.id))
            .map((s) => (
              <li key={s.id}>
                <Typography variant="body2">
                  <strong>{s.name}</strong>
                </Typography>
              </li>
            ))}
        </Box>
      </B4Dialog>

      <B4Dialog
        open={compareDialog.open && !compareDialog.setB}
        onClose={() =>
          setCompareDialog({ open: false, setA: null, setB: null })
        }
        title={t("sets.compareDialog.title")}
        subtitle={`${t("sets.compareDialog.comparingWith")}: ${compareDialog.setA?.name}`}
        icon={<CompareIcon />}
      >
        <List>
          {sets
            .filter((s) => s.id !== compareDialog.setA?.id)
            .map((s) => (
              <ListItem
                key={s.id}
                component="div"
                onClick={() =>
                  setCompareDialog((prev) => ({ ...prev, setB: s }))
                }
                sx={{
                  cursor: "pointer",
                  borderRadius: 1,
                  "&:hover": { bgcolor: colors.accent.primary },
                }}
              >
                <ListItemText primary={s.name} />
              </ListItem>
            ))}
        </List>
      </B4Dialog>

      <SetCompare
        open={compareDialog.open && !!compareDialog.setB}
        setA={compareDialog.setA}
        setB={compareDialog.setB}
        onClose={() =>
          setCompareDialog({ open: false, setA: null, setB: null })
        }
      />
    </Stack>
  );
};

interface StatItemProps {
  value: string | number;
  label: string;
  color: string;
  icon?: React.ReactNode;
}

const StatItem = ({ value, label, color, icon }: StatItemProps) => (
  <Stack direction="row" alignItems="center" spacing={1}>
    {icon && <Box sx={{ color, display: "flex" }}>{icon}</Box>}
    <Typography variant="h5" fontWeight={700} sx={{ color }}>
      {value}
    </Typography>
    <Typography variant="body2" color="text.secondary">
      {label}
    </Typography>
  </Stack>
);
