import {
  Box,
  Button,
  FormControlLabel,
  Grid,
  InputAdornment,
  Paper,
  Stack,
  Switch,
  TextField,
  Tooltip,
  Typography,
} from "@mui/material";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router";

import {
  AddIcon,
  CategoryIcon,
  CheckIcon,
  ClearIcon,
  DomainIcon,
  ImportExportIcon,
  SetsIcon,
  WarningIcon,
} from "@b4.icons";
import { ImportToolDialog } from "./ImportTool";
import SearchOutlinedIcon from "@mui/icons-material/SearchOutlined";

import {
  CollisionDetection,
  DndContext,
  DragMoveEvent,
  DragOverlay,
  DragStartEvent,
  MouseSensor,
  TouchSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  AnimateLayoutChanges,
  SortableContext,
  SortingStrategy,
  defaultAnimateLayoutChanges,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

import { B4Dialog, B4Help, B4Section } from "@b4.elements";
import { useSnackbar } from "@context/SnackbarProvider";
import {
  describeHubGroupVoteError,
  deviceFilterSelects,
  reportSaveError,
} from "@utils";

import { SetCompare } from "./Compare";
import { SetCard } from "./SetCard";
import { ExportTarget, SetExportDialog } from "./ExportDialog";
import { SetGroupHeader, SetGroupNameDialog } from "./SetGroups";
import { ShareDialog, ShareTarget } from "./ShareDialog";
import { EditorSection } from "./facets";
import { FacetCompareBar } from "./SignalRail";

import {
  GROUP_ROW_GAP,
  colors,
  fonts,
  groupTraySx,
  radius,
  radiusPx,
} from "@design";
import { configApi } from "@api/settings";
import { formatSetTemplate, setTemplateFromSet } from "@models/setTemplate";
import { useSets } from "@hooks/useSets";
import { useHubGroupVote, useHubGroups, useHubStatus } from "@hooks/useHub";
import { HubGroup, HubVoteKind } from "@models/hub";
import { useSetFacetSelection } from "@hooks/useSetFacetSelection";
import { useWatchdogSetStatuses } from "@hooks/useWatchdog";
import { B4Config, B4SetConfig } from "@models/config";
import { formatAsn } from "@models/asn";
import {
  DropResult,
  Point,
  SlotRect,
  cardDrop,
  groupDrop,
  leanOf,
  nearestSlot,
  setBlocks,
} from "@models/setGroups";
import { telegramSetId, telegramSetProblem } from "@models/telegramSet";
import { useTranslation } from "react-i18next";

export interface SetStats {
  manual_domains: number;
  manual_ips: number;
  geosite_domains: number;
  geoip_ips: number;
  total_domains: number;
  total_ips: number;
  asn_ips: number;
  geosite_category_breakdown?: Record<string, number>;
  geoip_category_breakdown?: Record<string, number>;
  asn_breakdown?: Record<string, number>;
  asn_unresolved?: string[];
}

export interface SetWithStats extends B4SetConfig {
  stats: SetStats;
}

const TEMP_ID_PREFIX = "temp-";
const isTempId = (id: string): boolean => id.startsWith(TEMP_ID_PREFIX);

export const setItemId = (item: SetWithStats): string => {
  const nested = item as unknown as { set?: B4SetConfig };
  return nested.set ? nested.set.id : item.id;
};

export const setItemWithSaved = (
  item: SetWithStats,
  saved: B4SetConfig,
): SetWithStats => {
  const nested = item as unknown as { set?: B4SetConfig };
  if (nested.set) {
    return { ...item, set: { ...nested.set, ...saved } } as SetWithStats;
  }
  return { ...item, ...saved };
};

const setItemWithEnabled = (
  item: SetWithStats,
  enabled: boolean,
): SetWithStats => {
  const nested = item as unknown as { set?: B4SetConfig };
  if (nested.set) {
    return { ...item, set: { ...nested.set, enabled } } as SetWithStats;
  }
  return { ...item, enabled };
};

const setItemWithGroup = (item: SetWithStats, group: string): SetWithStats => {
  const nested = item as unknown as { set?: B4SetConfig };
  if (nested.set) {
    return { ...item, set: { ...nested.set, group } } as SetWithStats;
  }
  return { ...item, group };
};

const arrangedItems = (
  data: SetWithStats[],
  ids: string[],
  groups: Record<string, string>,
): SetWithStats[] => {
  const byId = new Map(data.map((item) => [setItemId(item), item]));
  const ordered = ids.flatMap((id) => {
    const item = byId.get(id);
    return item ? [item] : [];
  });
  if (ordered.length !== data.length) return data;
  return ordered.map((item) => {
    const group = groups[setItemId(item)];
    return group === undefined ? item : setItemWithGroup(item, group);
  });
};

const sameDrop = (a: DropResult, b: DropResult): boolean =>
  a.group === b.group && a.ids.join("\n") === b.ids.join("\n");

interface SetsManagerProps {
  config: B4Config & { sets?: SetWithStats[] };
  onRefresh: () => void;
}

const noCollisions: CollisionDetection = () => [];
const noShift: SortingStrategy = () => null;
const animateReorder: AnimateLayoutChanges = (args) =>
  defaultAnimateLayoutChanges({ ...args, wasDragging: true });

const setMatches = (set: B4SetConfig, lower: string): boolean =>
  set.name.toLowerCase().includes(lower) ||
  (set.group ?? "").toLowerCase().includes(lower) ||
  !!set.targets?.sni_domains?.some((d) => d.toLowerCase().includes(lower)) ||
  !!set.targets?.geosite_categories?.some((c) =>
    c.toLowerCase().includes(lower),
  ) ||
  !!set.targets?.asns?.some((a) => formatAsn(a).toLowerCase().includes(lower));

interface GroupDialogState {
  rename: boolean;
  ids: string[];
  initial: string;
}

type HandleProps = React.HTMLAttributes<HTMLDivElement>;

interface DragSession {
  kind: "card" | "group";
  activeId: string;
  run: string[];
  base: B4SetConfig[];
  visible: string[];
  slots: SlotRect[];
  origin: Point;
  vertical: boolean;
}

interface SortableCardWrapperProps {
  id: string;
  tray?: { first: boolean; last: boolean };
  dimmed: boolean;
  outerRef?: (el: HTMLDivElement | null) => void;
  header?: (handleProps: HandleProps) => React.ReactNode;
  children: (handleProps: HandleProps) => React.JSX.Element;
}

const SortableCardWrapper = ({
  id,
  tray,
  dimmed,
  outerRef,
  header,
  children,
}: SortableCardWrapperProps) => {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id, animateLayoutChanges: animateReorder });
  const handleProps = { ...attributes, ...listeners };

  const combinedRef = (el: HTMLDivElement | null) => {
    setNodeRef(el);
    outerRef?.(el);
  };

  return (
    <Box
      ref={combinedRef}
      style={{
        transform: CSS.Translate.toString(transform),
        transition,
        opacity: isDragging || dimmed ? 0.4 : 1,
        zIndex: isDragging ? 1 : 0,
      }}
      sx={{
        position: "relative",
        height: "100%",
        ...(tray ? groupTraySx(tray.first, tray.last) : null),
      }}
    >
      {header?.(handleProps)}
      {children(handleProps)}
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
    setEnabledForSets,
    setGroupForSets,
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
  const [importToolOpen, setImportToolOpen] = useState(false);
  const [groupDialog, setGroupDialog] = useState<GroupDialogState | null>(
    null,
  );
  const [exportTarget, setExportTarget] = useState<ExportTarget | null>(null);
  const [shareTarget, setShareTarget] = useState<ShareTarget | null>(null);
  const [compareDialog, setCompareDialog] = useState<{
    open: boolean;
    setId: string | null;
  }>({ open: false, setId: null });
  const [templateSetId, setTemplateSetId] = useState<string | null>(null);
  const [savingTemplate, setSavingTemplate] = useState(false);

  const [drag, setDrag] = useState<Pick<
    DragSession,
    "kind" | "activeId" | "run"
  > | null>(null);
  const [arrangement, setArrangement] = useState<DropResult | null>(null);
  const dragRef = useRef<DragSession | null>(null);
  const dropRef = useRef<DropResult | null>(null);
  const [highlightedSetId, setHighlightedSetId] = useState<string | null>(null);
  const cardRefs = useRef(new Map<string, HTMLDivElement>());
  const tempIdSeq = useRef(0);

  const serverData = useMemo(() => config.sets || [], [config.sets]);
  const [setsData, setSetsData] = useState<SetWithStats[]>(serverData);
  const [syncingIds, setSyncingIds] = useState<Set<string>>(new Set());

  useEffect(() => {
    setSetsData(serverData);
  }, [serverData]);

  const markSyncing = useCallback((ids: string[], on: boolean) => {
    setSyncingIds((prev) => {
      const next = new Set(prev);
      for (const id of ids) {
        if (on) next.add(id);
        else next.delete(id);
      }
      return next;
    });
  }, []);

  const liveData = useMemo(
    () =>
      drag && arrangement
        ? arrangedItems(
            setsData,
            arrangement.ids,
            drag.kind === "card" ? { [drag.activeId]: arrangement.group } : {},
          )
        : setsData,
    [setsData, drag, arrangement],
  );
  const sets = useMemo(
    () => liveData.map((s) => ("set" in s ? s.set : s)) as B4SetConfig[],
    [liveData],
  );
  const setsStats = useMemo(
    () => liveData.map((s) => ("stats" in s ? s.stats : null)),
    [liveData],
  );
  const statsOf = useCallback(
    (id: string) => {
      const index = sets.findIndex((s) => s.id === id);
      return index >= 0 ? setsStats[index] || undefined : undefined;
    },
    [sets, setsStats],
  );
  const facetSelection = useSetFacetSelection(sets.map((s) => s.id));
  const watchdogOn = !!config.system?.checker?.watchdog?.enabled;
  const hubEnabled = Boolean(config.system?.hub?.enabled);
  const hubStatus = useHubStatus(hubEnabled);
  const hubReady = Boolean(
    hubStatus.data?.enabled && hubStatus.data?.configured,
  );
  const hubGroups = useHubGroups(hubReady);
  const listedGroupIds = useMemo(
    () => new Set((hubGroups.data?.groups ?? []).map((g) => g.id)),
    [hubGroups.data],
  );
  const publishedGroups = useMemo(
    () =>
      new Map(
        (hubGroups.data?.groups ?? [])
          .filter(
            (g) => g.applied.state === "applied" && g.applied.version === g.version,
          )
          .map((g) => [g.applied.group ?? "", g]),
      ),
    [hubGroups.data],
  );
  const groupVote = useHubGroupVote();
  const castGroupVote = async (hubGroup: HubGroup, kind: HubVoteKind) => {
    try {
      const res = await groupVote.mutateAsync({ id: hubGroup.id, kind });
      showSuccess(res.sent ? t("hub.vote.sent") : t("hub.vote.queued"));
    } catch (e) {
      showError(describeHubGroupVoteError(e, t));
    }
  };
  const groupVoteOf = (hubGroup: HubGroup | undefined) =>
    hubGroup
      ? {
          applied: hubGroup.applied,
          busy: groupVote.isPending,
          onVote: (kind: HubVoteKind) => void castGroupVote(hubGroup, kind),
        }
      : undefined;
  const deviceFilter = deviceFilterSelects(config.queue?.devices);
  const anyWatched = sets.some((s) => !!s.discovery?.watchdog);
  const watchStatuses = useWatchdogSetStatuses(anyWatched && watchdogOn);
  const telegramId = telegramSetId(config);
  const telegramState = useMemo(
    () =>
      telegramId === ""
        ? undefined
        : { problem: telegramSetProblem({ ...config, sets }) },
    [config, sets, telegramId],
  );

  const sensors = useSensors(
    useSensor(MouseSensor, {
      activationConstraint: { distance: 8 },
    }),
    useSensor(TouchSensor, {
      activationConstraint: { delay: 200, tolerance: 8 },
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

  const blocks = useMemo(() => setBlocks(sets), [sets]);
  const visibleBlocks = useMemo(() => {
    if (!filterText.trim()) return blocks;
    const lower = filterText.toLowerCase();
    return blocks
      .map((block) => ({
        ...block,
        sets: block.sets.filter((set) => setMatches(set, lower)),
      }))
      .filter((block) => block.sets.length > 0);
  }, [blocks, filterText]);
  const filteredSets = useMemo(
    () => visibleBlocks.flatMap((block) => block.sets),
    [visibleBlocks],
  );
  const groupNames = useMemo(
    () => [...new Set(sets.map((s) => s.group ?? "").filter(Boolean))],
    [sets],
  );

  const endDrag = () => {
    dragRef.current = null;
    dropRef.current = null;
    setDrag(null);
    setArrangement(null);
  };

  const handleDragStart = (event: DragStartEvent) => {
    const id = String(event.active.id);
    const visible = filteredSets.map((s) => s.id);
    const slots = visible.flatMap((setId) => {
      const el = cardRefs.current.get(setId);
      return el ? [el.getBoundingClientRect()] : [];
    });
    const home = slots[visible.indexOf(id)];
    if (slots.length !== visible.length || !home) return;
    const target = event.activatorEvent.target;
    const block =
      target instanceof Element && target.closest("[data-group-handle]")
        ? blocks.find((b) => b.sets.some((s) => s.id === id))
        : undefined;
    const session: DragSession = {
      kind: block ? "group" : "card",
      activeId: id,
      run: block ? block.sets.map((s) => s.id) : [id],
      base: sets,
      visible,
      slots,
      origin: {
        x: home.left + home.width / 2,
        y: home.top + home.height / 2,
      },
      vertical:
        slots.length > 1 && Math.abs(slots[0].left - slots[1].left) < 1,
    };
    dragRef.current = session;
    dropRef.current = null;
    setDrag({ kind: session.kind, activeId: id, run: session.run });
  };

  const handleDragMove = (event: DragMoveEvent) => {
    const session = dragRef.current;
    if (!session) return;
    const center = {
      x: session.origin.x + event.delta.x,
      y: session.origin.y + event.delta.y,
    };
    const slot = nearestSlot(center, session.slots);
    const next =
      session.kind === "group"
        ? {
            ids: groupDrop(session.base, session.visible, session.run, slot),
            group: "",
          }
        : cardDrop(
            session.base,
            session.visible,
            session.activeId,
            slot,
            leanOf(center, session.slots[slot], session.vertical),
          );
    if (dropRef.current && sameDrop(dropRef.current, next)) return;
    dropRef.current = next;
    setArrangement(next);
  };

  const handleDragEnd = () => {
    const session = dragRef.current;
    const result = dropRef.current;
    endDrag();
    if (!session || !result) return;
    const own =
      session.base.find((s) => s.id === session.activeId)?.group ?? "";
    const regrouped =
      session.kind === "card" &&
      result.group !== own &&
      !isTempId(session.activeId);
    const moved =
      result.ids.join("\n") !== session.base.map((s) => s.id).join("\n");
    if (!regrouped && !moved) return;
    const groups = regrouped
      ? { [session.activeId]: result.group }
      : undefined;
    setSetsData((prev) => arrangedItems(prev, result.ids, groups ?? {}));

    void (async () => {
      const response = await reorderSets(
        result.ids.filter((id) => !isTempId(id)),
        groups,
      );
      if (!response.success) {
        reportSaveError(response.error, showError, t, "sets.manager.failedToReorder");
      }
      onRefresh();
    })();
  };

  const activeSet = drag ? sets.find((s) => s.id === drag.activeId) : undefined;

  const applyGroup = (ids: string[], group: string, done: string) => {
    const real = ids.filter((id) => !isTempId(id));
    setGroupDialog(null);
    if (real.length === 0) return;
    void (async () => {
      const result = await setGroupForSets(real, group);
      if (result.success) showSuccess(done);
      else reportSaveError(result.error, showError, t, "sets.groups.failed");
      onRefresh();
    })();
  };

  const confirmGroupDialog = (name: string) => {
    if (!groupDialog) return;
    const { ids, rename } = groupDialog;
    if (!rename) handleExitSelectionMode();
    applyGroup(
      ids,
      name,
      rename
        ? t("sets.groups.renamed", { group: name })
        : t("sets.groups.grouped", { count: ids.length, group: name }),
    );
  };

  const removeFromGroup = () => {
    if (!groupDialog) return;
    const { ids } = groupDialog;
    handleExitSelectionMode();
    applyGroup(ids, "", t("sets.groups.removed", { count: ids.length }));
  };

  const openGroupDialog = () => {
    const picked = sets.filter((s) => selectedIds.has(s.id));
    const groups = new Set(picked.map((s) => s.group ?? ""));
    setGroupDialog({
      rename: false,
      ids: picked.map((s) => s.id),
      initial: groups.size === 1 ? [...groups][0] : "",
    });
  };

  const handleToggleBlock = (members: B4SetConfig[], enabled: boolean) => {
    const ids = members.map((s) => s.id).filter((id) => !isTempId(id));
    if (ids.length === 0) return;
    const idSet = new Set(ids);
    setSetsData((prev) =>
      prev.map((s) =>
        idSet.has(setItemId(s)) ? setItemWithEnabled(s, enabled) : s,
      ),
    );
    markSyncing(ids, true);
    void (async () => {
      const result = await setEnabledForSets(ids, enabled);
      markSyncing(ids, false);
      if (!result.success) {
        reportSaveError(result.error, showError, t, "sets.manager.failedToToggleSets");
      }
      onRefresh();
    })();
  };

  const handleAddSet = () => {
    navigate("/sets/new")?.catch(() => {});
  };

  const handleEditSet = (set: B4SetConfig, section?: EditorSection) => {
    const params = new URLSearchParams();
    if (section?.tab) params.set("tab", section.tab);
    if (section?.sub) params.set("sub", section.sub);
    const query = params.toString();
    navigate(`/sets/${set.id}${query ? `?${query}` : ""}`)?.catch(() => {});
  };

  const handleDeleteSet = () => {
    const { setId } = deleteDialog;
    if (!setId) return;
    setDeleteDialog({ open: false, setId: null });
    setSetsData((prev) => prev.filter((s) => setItemId(s) !== setId));
    if (isTempId(setId)) return;
    void (async () => {
      const result = await deleteSet(setId);
      if (result.success) {
        showSuccess(t("sets.manager.setDeleted"));
        onRefresh();
      } else {
        reportSaveError(result.error, showError, t, "sets.manager.failedToDelete");
        onRefresh();
      }
    })();
  };

  const templateSet = templateSetId
    ? (sets.find((s) => s.id === templateSetId) ?? null)
    : null;
  const templatePreview = templateSet ? setTemplateFromSet(templateSet) : null;
  const templateText = formatSetTemplate(templatePreview);

  const closeTemplateDialog = () => {
    if (!savingTemplate) setTemplateSetId(null);
  };

  const handleUseForNewSets = () => {
    if (!templatePreview) return;
    setSavingTemplate(true);
    void (async () => {
      try {
        await configApi.saveNewSetTemplate(templatePreview);
        showSuccess(t("sets.templateDialog.saved"));
        setTemplateSetId(null);
        onRefresh();
      } catch (error) {
        reportSaveError(error, showError, t, "sets.templateDialog.failed");
      } finally {
        setSavingTemplate(false);
      }
    })();
  };

  const handleDuplicateSet = (set: B4SetConfig) => {
    if (isTempId(set.id)) return;
    tempIdSeq.current += 1;
    const tempId = `${TEMP_ID_PREFIX}${tempIdSeq.current}`;
    const srcIndex = setsData.findIndex((s) => setItemId(s) === set.id);
    const srcStats = srcIndex >= 0 ? setsStats[srcIndex] : null;
    const placeholder = {
      ...set,
      id: tempId,
      name: `${set.name} (copy)`,
      stats: srcStats ?? undefined,
    } as unknown as SetWithStats;

    setSetsData((prev) => {
      const insertAt = srcIndex >= 0 ? srcIndex + 1 : prev.length;
      const next = [...prev];
      next.splice(insertAt, 0, placeholder);
      return next;
    });
    markSyncing([tempId], true);

    void (async () => {
      const result = await duplicateSet(set);
      markSyncing([tempId], false);
      if (result.success) {
        showSuccess(t("sets.manager.setDuplicated"));
        onRefresh();
      } else {
        setSetsData((prev) => prev.filter((s) => setItemId(s) !== tempId));
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
    const ids = Array.from(selectedIds).filter((id) => !isTempId(id));
    const count = ids.length;
    setBatchDeleteDialog(false);
    handleExitSelectionMode();
    if (count === 0) return;
    const idSet = new Set(ids);
    setSetsData((prev) => prev.filter((s) => !idSet.has(setItemId(s))));
    void (async () => {
      const result = await deleteSets(ids);
      if (result.success) {
        showSuccess(t("sets.manager.setsDeleted", { count }));
        onRefresh();
      } else {
        reportSaveError(result.error, showError, t, "sets.manager.failedToDeleteSets");
        onRefresh();
      }
    })();
  };

  const handleToggleAll = (enabled: boolean) => {
    const ids = setsData
      .map((s) => setItemId(s))
      .filter((id) => !isTempId(id));
    if (ids.length === 0) return;
    setSetsData((prev) => prev.map((s) => setItemWithEnabled(s, enabled)));
    markSyncing(ids, true);
    void (async () => {
      const result = await setEnabledForSets(ids, enabled);
      markSyncing(ids, false);
      if (result.success) {
        showSuccess(
          t(enabled ? "sets.manager.allEnabled" : "sets.manager.allDisabled"),
        );
        onRefresh();
      } else {
        reportSaveError(result.error, showError, t, "sets.manager.failedToToggleSets");
        onRefresh();
      }
    })();
  };

  const handleToggleEnabled = (set: B4SetConfig, enabled: boolean) => {
    setSetsData((prev) =>
      prev.map((s) =>
        setItemId(s) === set.id ? setItemWithEnabled(s, enabled) : s,
      ),
    );
    if (isTempId(set.id)) return;
    markSyncing([set.id], true);
    void (async () => {
      const result = await setEnabledForSets([set.id], enabled);
      markSyncing([set.id], false);
      if (!result.success) {
        reportSaveError(result.error, showError, t, "sets.manager.failedToUpdate");
      }
      onRefresh();
    })();
  };

  const renderCell = (
    set: B4SetConfig,
    block: (typeof visibleBlocks)[number],
    position: number,
  ) => {
    const index = sets.findIndex((s) => s.id === set.id);
    const grouped = block.group !== "";
    return (
      <SortableCardWrapper
        id={set.id}
        outerRef={registerCardRef(set.id)}
        dimmed={drag?.kind === "group" && drag.run.includes(set.id)}
        tray={
          grouped
            ? {
                first: position === 0,
                last: position === block.sets.length - 1,
              }
            : undefined
        }
        header={
          grouped && position === 0
            ? (handleProps) => (
                <SetGroupHeader
                  group={block.group}
                  count={block.all.length}
                  enabledCount={block.all.filter((s) => s.enabled).length}
                  selectionMode={selectionMode}
                  handleProps={handleProps}
                  onToggleEnabled={(enabled) =>
                    handleToggleBlock(block.all, enabled)
                  }
                  onRename={() =>
                    setGroupDialog({
                      rename: true,
                      ids: block.all.map((s) => s.id),
                      initial: block.group,
                    })
                  }
                  onExport={() =>
                    setExportTarget({ group: block.group, sets: block.all })
                  }
                  onShare={
                    hubReady && !block.all.some((s) => isTempId(s.id))
                      ? () =>
                          setShareTarget({
                            group: block.group,
                            sets: block.all,
                          })
                      : undefined
                  }
                  vote={groupVoteOf(publishedGroups.get(block.group))}
                  onUngroup={() =>
                    applyGroup(
                      block.all.map((s) => s.id),
                      "",
                      t("sets.groups.ungrouped", { group: block.group }),
                    )
                  }
                />
              )
            : undefined
        }
      >
        {(dragHandleProps) => (
          <SetCard
            set={set}
            stats={setsStats[index] || undefined}
            index={index}
            onEdit={(section?: EditorSection) => handleEditSet(set, section)}
            onDuplicate={() => handleDuplicateSet(set)}
            onCompare={() => setCompareDialog({ open: true, setId: set.id })}
            onExport={() => setExportTarget({ set })}
            onShare={
              hubEnabled && !isTempId(set.id)
                ? () => setShareTarget({ set })
                : undefined
            }
            onUseForNewSets={() => setTemplateSetId(set.id)}
            onDelete={() => setDeleteDialog({ open: true, setId: set.id })}
            groups={groupNames}
            onMoveToGroup={(group) =>
              applyGroup(
                [set.id],
                group,
                t("sets.groups.grouped", { count: 1, group }),
              )
            }
            onNewGroup={() =>
              setGroupDialog({ rename: false, ids: [set.id], initial: "" })
            }
            onRemoveFromGroup={() =>
              applyGroup([set.id], "", t("sets.groups.removed", { count: 1 }))
            }
            onToggleEnabled={(enabled) => handleToggleEnabled(set, enabled)}
            syncing={syncingIds.has(set.id)}
            dragHandleProps={dragHandleProps}
            selectionMode={selectionMode}
            selected={selectedIds.has(set.id)}
            onSelect={() => handleToggleSelection(set.id)}
            escalatesTo={escalationMaps.escalatesTo.get(set.id)}
            escalatedFrom={escalationMaps.escalatedFrom.get(set.id)}
            highlighted={highlightedSetId === set.id}
            onEscalationHover={handleEscalationHover}
            onEscalationClick={handleEscalationClick}
            activeFacet={facetSelection.open[set.id] ?? null}
            onFacetSelect={(key) => {
              facetSelection.selectFacet(set.id, key);
            }}
            onVoted={onRefresh}
            watchStatus={watchStatuses.byId.get(set.id)}
            watchdogOn={watchdogOn}
            deviceFilter={deviceFilter}
            telegramSet={set.id === telegramId ? telegramState : undefined}
            ratedByGroup={
              Boolean(set.group) && listedGroupIds.has(set.hub?.group_id ?? "")
            }
          />
        )}
      </SortableCardWrapper>
    );
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
            <Stack
              direction="row"
              spacing={4}
              flexWrap="wrap"
              useFlexGap
              sx={{ rowGap: 1.5 }}
            >
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

            <Stack
              direction="row"
              spacing={2}
              alignItems="center"
              flexWrap="wrap"
              useFlexGap
            >
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
                  width: { xs: "100%", sm: 200 },
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
                    variant="outlined"
                    startIcon={<CategoryIcon />}
                    disabled={selectedIds.size === 0}
                    onClick={openGroupDialog}
                  >
                    {t("sets.groups.group")}
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
                    startIcon={<ImportExportIcon />}
                    onClick={() => setImportToolOpen(true)}
                    variant="outlined"
                    size="small"
                  >
                    {t("sets.manager.importFromTool")}
                  </Button>
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

        {sets.length > 1 && (
          <Box sx={{ display: "flex", alignItems: "center", gap: 0.5, mb: 2 }}>
            <Typography variant="body2" sx={{ color: "text.secondary" }}>
              {t("sets.manager.orderHint")}
            </Typography>
            <B4Help
              label={t("sets.manager.orderTitle")}
              info={t("sets.manager.orderInfo")}
              learnMore="concepts#set"
            />
          </Box>
        )}

        {filteredSets.length > 0 && (
          <FacetCompareBar
            active={facetSelection.compare}
            onPick={(facet) => {
              facetSelection.pickCompare(
                facet,
                filteredSets.map((s) => s.id),
              );
            }}
            toggle={facetSelection.toggle}
            onToggle={facetSelection.toggleAll}
          />
        )}

        <DndContext
          sensors={sensors}
          collisionDetection={noCollisions}
          onDragStart={handleDragStart}
          onDragMove={handleDragMove}
          onDragEnd={handleDragEnd}
          onDragCancel={endDrag}
        >
          <SortableContext
            items={filteredSets.map((s) => s.id)}
            strategy={noShift}
          >
            <Grid
              container
              columnSpacing={3}
              rowSpacing={groupNames.length > 0 ? `${GROUP_ROW_GAP}px` : 3}
              sx={groupNames.length > 0 ? { mt: `${GROUP_ROW_GAP - 12}px` } : null}
            >
              {visibleBlocks.flatMap((block) =>
                block.sets.map((set, position) => (
                  <Grid key={set.id} size={{ xs: 12, sm: 6, lg: 4, xl: 3 }}>
                    {renderCell(set, block, position)}
                  </Grid>
                )),
              )}
            </Grid>
          </SortableContext>

          <DragOverlay>
            {drag && activeSet ? (
              <Box
                sx={{
                  height: "100%",
                  p: 3,
                  bgcolor: colors.background.paper,
                  border: `2px solid ${colors.secondary}`,
                  borderRadius: radius.md,
                  boxShadow:
                    drag.kind === "group"
                      ? `7px 7px 0 -2px ${colors.background.paper}, 7px 7px 0 0 ${colors.secondary}, 14px 14px 0 -2px ${colors.background.paper}, 14px 14px 0 0 ${colors.secondary}`
                      : `0 16px 48px ${colors.accent.primary}60`,
                }}
              >
                <Typography variant="h6" fontWeight={600} noWrap>
                  {drag.kind === "group" ? activeSet.group : activeSet.name}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {drag.kind === "group"
                    ? t("sets.groups.setCount", { count: drag.run.length })
                    : activeSet.fragmentation.strategy.toUpperCase()}
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
        open={templateSetId !== null}
        title={t("sets.templateDialog.title")}
        subtitle={templateSet?.name}
        icon={<SetsIcon />}
        onClose={closeTemplateDialog}
        actions={
          <>
            <Button onClick={closeTemplateDialog} disabled={savingTemplate}>
              {t("core.cancel")}
            </Button>
            <Box sx={{ flex: 1 }} />
            <Button
              onClick={handleUseForNewSets}
              variant="contained"
              disabled={savingTemplate || !templateSet}
            >
              {t("sets.templateDialog.confirm")}
            </Button>
          </>
        }
      >
        <Typography sx={{ mt: 2, mb: 1.5 }}>
          {templateText
            ? t("sets.templateDialog.intro")
            : t("sets.templateDialog.empty")}
        </Typography>
        {templateText && (
          <Box
            component="pre"
            sx={{
              m: 0,
              p: "12px 13px",
              maxHeight: 320,
              overflow: "auto",
              border: `1px solid ${colors.border.light}`,
              borderRadius: `${radiusPx.sm}px`,
              bgcolor: colors.background.default,
              fontFamily: fonts.mono,
              fontSize: "0.72rem",
              lineHeight: 1.7,
              color: colors.text.secondary,
            }}
          >
            {templateText}
          </Box>
        )}
        <Typography variant="body2" color="text.secondary" sx={{ mt: 1.5 }}>
          {t("sets.templateDialog.note")}
        </Typography>
      </B4Dialog>

      {groupDialog && (
        <SetGroupNameDialog
          title={
            groupDialog.rename
              ? t("sets.groups.renameTitle")
              : t("sets.groups.groupTitle", { count: groupDialog.ids.length })
          }
          subtitle={
            groupDialog.rename ? undefined : t("sets.groups.groupSubtitle")
          }
          initial={groupDialog.initial}
          groups={groupNames}
          confirmLabel={
            groupDialog.rename ? t("sets.groups.rename") : t("sets.groups.group")
          }
          onConfirm={confirmGroupDialog}
          onRemove={
            !groupDialog.rename &&
            sets.some((s) => groupDialog.ids.includes(s.id) && s.group)
              ? removeFromGroup
              : undefined
          }
          onClose={() => setGroupDialog(null)}
        />
      )}

      <SetExportDialog
        target={exportTarget}
        onClose={() => setExportTarget(null)}
      />

      {shareTarget && (
        <ShareDialog
          target={shareTarget}
          hubReady={hubReady}
          onPublished={onRefresh}
          onClose={() => setShareTarget(null)}
        />
      )}

      <ImportToolDialog
        open={importToolOpen}
        onClose={() => setImportToolOpen(false)}
        onImported={onRefresh}
      />

      <SetCompare
        open={compareDialog.open}
        sets={sets}
        statsOf={statsOf}
        initialA={compareDialog.setId}
        initialB={null}
        deviceFilter={deviceFilter}
        onClose={() => setCompareDialog({ open: false, setId: null })}
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
