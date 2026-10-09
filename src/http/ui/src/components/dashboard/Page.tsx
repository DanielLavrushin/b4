import { memo, useCallback, useMemo, useRef, useState } from "react";
import { Box, Container } from "@mui/material";
import {
  DndContext,
  DragOverlay,
  KeyboardSensor,
  MeasuringStrategy,
  PointerSensor,
  closestCenter,
  pointerWithin,
  useSensor,
  useSensors,
  type Announcements,
  type CollisionDetection,
  type DragEndEvent,
  type DragMoveEvent,
  type DragStartEvent,
  type UniqueIdentifier,
} from "@dnd-kit/core";
import { SortableContext, sortableKeyboardCoordinates } from "@dnd-kit/sortable";
import { useTranslation } from "react-i18next";
import { defaultSpanOf, useDashboardLayout } from "@hooks/useDashboardLayout";
import { RestartDialog } from "@components/settings/RestartDialog";
import { useMetrics, useMetricsFrame } from "@/stores/useMetrics";
import { Attention } from "./Attention";
import { CustomizeBar, type HiddenPanelEntry } from "./CustomizeBar";
import { EngineFailureCard } from "./EngineFailureCard";
import {
  clampDelta,
  moveBeside,
  moveToNewRow,
  resizePair,
  rowBreakOf,
  shownRows,
  toggleRowBreak,
  type ArrangeContext,
  type DropSide,
  type RowCell,
} from "./layoutRows";
import { LinkLine } from "./LinkLine";
import {
  ColumnGuides,
  GAP_PREFIX,
  GRID_CONTAINER,
  GRID_GAP,
  PanelFrame,
  PanelGhost,
  RowGap,
  WIDE_GRID,
} from "./PanelFrame";
import { usePanelAvailability } from "./panels";
import { MIN_SPAN, PANELS_BY_ID, isPanelAvailable } from "./registry";
import { StatusStrip } from "./StatusStrip";

const WIDE_MIN_PX = 960;

const isGap = (id: UniqueIdentifier): boolean => String(id).startsWith(GAP_PREFIX);

const panelCollision: CollisionDetection = (args) => {
  const hits = pointerWithin(args);
  if (hits.length > 0) return hits;
  return closestCenter({
    ...args,
    droppableContainers: args.droppableContainers.filter((c) => !isGap(c.id)),
  });
};

const noSorting = () => null;

type DropTarget =
  | { kind: "panel"; id: string; side: DropSide }
  | { kind: "gap"; before: string | null };

const sameTarget = (a: DropTarget | null, b: DropTarget | null): boolean => {
  if (a === null || b === null) return a === b;
  if (a.kind === "panel" && b.kind === "panel") return a.id === b.id && a.side === b.side;
  if (a.kind === "gap" && b.kind === "gap") return a.before === b.before;
  return false;
};

interface ResizePreview {
  id: string;
  delta: number;
}

const withPreview = (rows: RowCell[][], preview: ResizePreview | null): RowCell[][] => {
  if (!preview) return rows;
  return rows.map((cells) => {
    const i = cells.findIndex((cell) => cell.id === preview.id);
    if (i < 0 || i + 1 >= cells.length) return cells;
    const d = clampDelta(cells[i].span, cells[i + 1].span, preview.delta);
    if (d === 0) return cells;
    return cells.map((cell, j) => {
      if (j === i) return { ...cell, span: cell.span + d };
      if (j === i + 1) return { ...cell, span: cell.span - d };
      return cell;
    });
  });
};

interface DashboardGridProps {
  editing: boolean;
  onDone: () => void;
}

const DashboardGrid = memo(function DashboardGrid({
  editing,
  onDone,
}: DashboardGridProps) {
  const { t } = useTranslation();
  const gridRef = useRef<HTMLDivElement | null>(null);
  const wideDrag = useRef(true);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [drop, setDrop] = useState<DropTarget | null>(null);
  const [preview, setPreview] = useState<ResizePreview | null>(null);
  const availability = usePanelAvailability();
  const { order, breaks, hidden, spans, arrange, setHidden, reset, customized } =
    useDashboardLayout();

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const ctx: ArrangeContext = useMemo(
    () => ({
      visible: (id: string) => {
        const panel = PANELS_BY_ID.get(id);
        return !!panel && !hidden.has(id) && isPanelAvailable(panel, availability);
      },
      defaultSpan: defaultSpanOf,
    }),
    [hidden, availability],
  );

  const arrangement = useMemo(() => ({ order, breaks, spans }), [order, breaks, spans]);
  const rows = useMemo(() => shownRows(arrangement, ctx), [arrangement, ctx]);
  const displayRows = useMemo(() => withPreview(rows, preview), [rows, preview]);
  const flatIds = useMemo(() => rows.flat().map((cell) => cell.id), [rows]);

  const hiddenPanels: HiddenPanelEntry[] = useMemo(
    () =>
      order
        .filter((id) => hidden.has(id))
        .map((id) => PANELS_BY_ID.get(id))
        .filter((panel) => panel !== undefined)
        .map((panel) => ({
          id: panel.id,
          title: t(panel.titleKey),
          available: isPanelAvailable(panel, availability),
        })),
    [order, hidden, availability, t],
  );

  const titleOf = useCallback(
    (id: UniqueIdentifier) => {
      const panel = PANELS_BY_ID.get(String(id));
      return panel ? t(panel.titleKey) : String(id);
    },
    [t],
  );

  const announcements: Announcements = useMemo(
    () => ({
      onDragStart: ({ active }) =>
        t("dashboard.customize.announce.start", { title: titleOf(active.id) }),
      onDragOver: ({ active, over }) => {
        if (!over) {
          return t("dashboard.customize.announce.outside", { title: titleOf(active.id) });
        }
        if (over.id === active.id) return undefined;
        if (isGap(over.id)) {
          return t("dashboard.customize.announce.overGap", { title: titleOf(active.id) });
        }
        return t("dashboard.customize.announce.over", {
          title: titleOf(active.id),
          target: titleOf(over.id),
        });
      },
      onDragEnd: ({ active, over }) => {
        if (!over || over.id === active.id) {
          return t("dashboard.customize.announce.cancel", { title: titleOf(active.id) });
        }
        if (isGap(over.id)) {
          return t("dashboard.customize.announce.endGap", { title: titleOf(active.id) });
        }
        return t("dashboard.customize.announce.end", {
          title: titleOf(active.id),
          target: titleOf(over.id),
        });
      },
      onDragCancel: ({ active }) =>
        t("dashboard.customize.announce.cancel", { title: titleOf(active.id) }),
    }),
    [t, titleOf],
  );

  const accessibility = useMemo(
    () => ({
      announcements,
      screenReaderInstructions: {
        draggable: t("dashboard.customize.announce.instructions"),
      },
    }),
    [announcements, t],
  );

  const targetOf = (event: DragMoveEvent | DragEndEvent): DropTarget | null => {
    const { active, over, activatorEvent, delta } = event;
    if (!over) return null;
    const overId = String(over.id);
    if (isGap(overId)) return { kind: "gap", before: overId.slice(GAP_PREFIX.length) || null };
    if (overId === String(active.id)) return null;
    let side: DropSide;
    if (activatorEvent instanceof MouseEvent) {
      const x = activatorEvent.clientX + delta.x;
      const y = activatorEvent.clientY + delta.y;
      const r = over.rect;
      side = wideDrag.current
        ? x < r.left + r.width / 2
          ? "before"
          : "after"
        : y < r.top + r.height / 2
          ? "before"
          : "after";
    } else {
      side = flatIds.indexOf(String(active.id)) < flatIds.indexOf(overId) ? "after" : "before";
    }
    return { kind: "panel", id: overId, side };
  };

  const handleDragStart = (event: DragStartEvent) => {
    wideDrag.current = (gridRef.current?.clientWidth ?? 0) >= WIDE_MIN_PX;
    setActiveId(String(event.active.id));
  };

  const handleDragMove = (event: DragMoveEvent) => {
    const next = targetOf(event);
    setDrop((prev) => (sameTarget(prev, next) ? prev : next));
  };

  const handleDragEnd = (event: DragEndEvent) => {
    const id = String(event.active.id);
    const target = targetOf(event);
    setActiveId(null);
    setDrop(null);
    if (target?.kind === "panel") {
      arrange((current) => moveBeside(current, ctx, id, target.id, target.side));
    } else if (target?.kind === "gap") {
      arrange((current) => moveToNewRow(current, ctx, id, target.before));
    }
  };

  const cancelDrag = () => {
    setActiveId(null);
    setDrop(null);
  };

  const resizeBy = (leftId: string, delta: number) =>
    arrange((current) => resizePair(current, ctx, leftId, delta));

  const commitResize = (id: string, delta: number) => {
    setPreview(null);
    if (delta !== 0) resizeBy(id, delta);
  };

  const panelRow = (r: number) => (editing ? 2 * r + 2 : r + 1);
  const dragging = activeId !== null;
  const activeTitle = activeId ? titleOf(activeId) : null;

  return (
    <>
      <CustomizeBar
        editing={editing}
        customized={customized}
        hiddenPanels={hiddenPanels}
        onShow={(id) => setHidden(id, false)}
        onReset={reset}
        onDone={onDone}
      />

      <DndContext
        sensors={sensors}
        collisionDetection={panelCollision}
        measuring={{ droppable: { strategy: MeasuringStrategy.Always } }}
        accessibility={accessibility}
        onDragStart={handleDragStart}
        onDragMove={handleDragMove}
        onDragCancel={cancelDrag}
        onDragEnd={handleDragEnd}
      >
        <SortableContext items={flatIds} strategy={noSorting}>
          <Box sx={{ containerType: "inline-size", containerName: GRID_CONTAINER }}>
            <Box
              ref={gridRef}
              sx={{
                position: "relative",
                display: "grid",
                gridTemplateColumns: "repeat(12, minmax(0, 1fr))",
                alignItems: "stretch",
                columnGap: `${GRID_GAP}px`,
                rowGap: `${GRID_GAP}px`,
                [WIDE_GRID]: { rowGap: editing ? 0 : `${GRID_GAP}px` },
              }}
            >
              {preview && <ColumnGuides />}
              {displayRows.flatMap((cells, r) => [
                editing ? (
                  <RowGap
                    key={`gap-${cells[0].id}`}
                    id={`${GAP_PREFIX}${cells[0].id}`}
                    row={2 * r + 1}
                    dragging={dragging}
                  />
                ) : null,
                ...cells.map((cell, i) => {
                  const panel = PANELS_BY_ID.get(cell.id);
                  if (!panel) return null;
                  const left = cells[i - 1];
                  const right = cells[i + 1];
                  const Component = panel.Component;
                  const grow = () => {
                    if (right) resizeBy(cell.id, 1);
                    else if (left) resizeBy(left.id, -1);
                  };
                  const shrink = () => {
                    if (right) resizeBy(cell.id, -1);
                    else if (left) resizeBy(left.id, 1);
                  };
                  return (
                    <PanelFrame
                      key={cell.id}
                      id={cell.id}
                      title={titleOf(cell.id)}
                      span={cell.span}
                      row={panelRow(r)}
                      editing={editing}
                      dropSide={
                        drop?.kind === "panel" && drop.id === cell.id && activeId !== cell.id
                          ? drop.side
                          : null
                      }
                      resizable={right !== undefined}
                      canGrow={right ? right.span > MIN_SPAN : left !== undefined && left.span > MIN_SPAN}
                      canShrink={cell.span > MIN_SPAN && (right !== undefined || left !== undefined)}
                      rowBreak={rowBreakOf(arrangement, ctx, cell.id)}
                      onGrow={grow}
                      onShrink={shrink}
                      onResizeMove={(delta) => setPreview({ id: cell.id, delta })}
                      onResizeEnd={(delta) => commitResize(cell.id, delta)}
                      onToggleRowBreak={() => arrange((current) => toggleRowBreak(current, ctx, cell.id))}
                      onHide={() => setHidden(cell.id, true)}
                    >
                      <Component />
                    </PanelFrame>
                  );
                }),
              ])}
              {editing && (
                <RowGap
                  id={GAP_PREFIX}
                  row={2 * displayRows.length + 1}
                  dragging={dragging}
                />
              )}
            </Box>
          </Box>
        </SortableContext>

        <DragOverlay dropAnimation={null}>
          {activeTitle ? <PanelGhost title={activeTitle} /> : null}
        </DragOverlay>
      </DndContext>
    </>
  );
});


export function DashboardPage() {
  const hasFrame = useMetrics((s) => s.frame !== null);
  const failure = useMetricsFrame((f) => f.engine_failure);
  const [editing, setEditing] = useState(false);
  const [restartOpen, setRestartOpen] = useState(false);

  const toggleEditing = useCallback(() => setEditing((prev) => !prev), []);
  const stopEditing = useCallback(() => setEditing(false), []);
  const openRestart = useCallback(() => setRestartOpen(true), []);

  return (
    <Container maxWidth={false} sx={{ p: 2 }}>
      {hasFrame && (
        <StatusStrip editing={editing} onToggleEditing={toggleEditing} />
      )}
      <LinkLine />
      {failure && <EngineFailureCard failure={failure} onRestart={openRestart} />}
      {hasFrame && <Attention onRestart={openRestart} />}
      <RestartDialog open={restartOpen} onClose={() => setRestartOpen(false)} />
      {hasFrame && <DashboardGrid editing={editing} onDone={stopEditing} />}
    </Container>
  );
}
