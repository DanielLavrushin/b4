import { memo, useCallback, useMemo, useState } from "react";
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
  type DragOverEvent,
  type DragStartEvent,
  type UniqueIdentifier,
} from "@dnd-kit/core";
import { SortableContext, sortableKeyboardCoordinates } from "@dnd-kit/sortable";
import { useTranslation } from "react-i18next";
import { useDashboardLayout } from "@hooks/useDashboardLayout";
import { RestartDialog } from "@components/settings/RestartDialog";
import { useMetrics, useMetricsFrame } from "@/stores/useMetrics";
import { Attention } from "./Attention";
import { CustomizeBar, type HiddenPanelEntry } from "./CustomizeBar";
import { EngineFailureCard } from "./EngineFailureCard";
import { LinkLine } from "./LinkLine";
import {
  ColumnGuides,
  GRID_CONTAINER,
  GRID_GAP,
  PanelFrame,
  PanelGhost,
  ROW_UNIT,
} from "./PanelFrame";
import { usePanelAvailability } from "./panels";
import { PANELS_BY_ID, isPanelAvailable } from "./registry";
import { StatusStrip } from "./StatusStrip";

const panelCollision: CollisionDetection = (args) => {
  const hits = pointerWithin(args);
  return hits.length > 0 ? hits : closestCenter(args);
};

const noSorting = () => null;

interface DashboardGridProps {
  editing: boolean;
  onDone: () => void;
}

const DashboardGrid = memo(function DashboardGrid({
  editing,
  onDone,
}: DashboardGridProps) {
  const { t } = useTranslation();
  const [activeId, setActiveId] = useState<string | null>(null);
  const [overId, setOverId] = useState<string | null>(null);
  const [resizingPanel, setResizingPanel] = useState(false);
  const availability = usePanelAvailability();
  const { order, hidden, spans, move, setSpan, setHidden, reset, customized } =
    useDashboardLayout();

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const visiblePanels = useMemo(
    () =>
      order
        .map((id) => PANELS_BY_ID.get(id))
        .filter((panel) => panel !== undefined)
        .filter((panel) => !hidden.has(panel.id))
        .filter((panel) => isPanelAvailable(panel, availability))
        .map((panel) => ({
          id: panel.id,
          title: t(panel.titleKey),
          span: spans[panel.id] ?? panel.defaultSpan,
          Component: panel.Component,
        })),
    [order, hidden, spans, availability, t],
  );

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
        return t("dashboard.customize.announce.over", {
          title: titleOf(active.id),
          target: titleOf(over.id),
        });
      },
      onDragEnd: ({ active, over }) =>
        over && over.id !== active.id
          ? t("dashboard.customize.announce.end", {
              title: titleOf(active.id),
              target: titleOf(over.id),
            })
          : t("dashboard.customize.announce.cancel", { title: titleOf(active.id) }),
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

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    setActiveId(null);
    setOverId(null);
    if (over && active.id !== over.id) {
      move(String(active.id), String(over.id));
    }
  };

  const cancelDrag = () => {
    setActiveId(null);
    setOverId(null);
  };

  const activePanel = visiblePanels.find((panel) => panel.id === activeId);

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
        onDragStart={(event: DragStartEvent) => setActiveId(String(event.active.id))}
        onDragOver={(event: DragOverEvent) =>
          setOverId(event.over ? String(event.over.id) : null)
        }
        onDragCancel={cancelDrag}
        onDragEnd={handleDragEnd}
      >
        <SortableContext
          items={visiblePanels.map((panel) => panel.id)}
          strategy={noSorting}
        >
          <Box sx={{ containerType: "inline-size", containerName: GRID_CONTAINER }}>
            <Box
              sx={{
                position: "relative",
                display: "grid",
                gridTemplateColumns: "repeat(12, minmax(0, 1fr))",
                gridAutoRows: `${ROW_UNIT}px`,
                gridAutoFlow: "row dense",
                alignItems: "start",
                columnGap: `${GRID_GAP}px`,
                rowGap: 0,
              }}
            >
              {resizingPanel && <ColumnGuides />}
              {visiblePanels.map(({ id, title, span, Component }) => (
                <PanelFrame
                  key={id}
                  id={id}
                  title={title}
                  span={span}
                  editing={editing}
                  dropTarget={overId === id && activeId !== id}
                  onSpanChange={(value) => setSpan(id, value)}
                  onResizeActive={setResizingPanel}
                  onHide={() => setHidden(id, true)}
                >
                  <Component />
                </PanelFrame>
              ))}
            </Box>
          </Box>
        </SortableContext>

        <DragOverlay dropAnimation={null}>
          {activePanel ? <PanelGhost title={activePanel.title} /> : null}
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
