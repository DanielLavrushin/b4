import { useEffect, useMemo, useState } from "react";
import { Box, Container, Typography, LinearProgress } from "@mui/material";
import {
  CollisionDetection,
  DndContext,
  DragEndEvent,
  DragOverEvent,
  DragOverlay,
  DragStartEvent,
  MeasuringStrategy,
  PointerSensor,
  closestCenter,
  pointerWithin,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import { SortableContext } from "@dnd-kit/sortable";
import { useTranslation } from "react-i18next";
import { HealthBanner } from "./HealthBanner";
import { CustomizeBar, HiddenPanelEntry } from "./CustomizeBar";
import { ColumnGuides, PanelFrame, PanelGhost, ROW_UNIT } from "./PanelFrame";
import { PANELS_BY_ID, PanelContext } from "./registry";
import { normalizeMetrics } from "./normalize";
import { useDashboardSets } from "@hooks/useDashboardSets";
import { useDashboardLayout } from "@hooks/useDashboardLayout";
import { wsUrl } from "@utils";
import type { Metrics } from "./types";

export * from "./types";

const panelCollision: CollisionDetection = (args) => {
  const hits = pointerWithin(args);
  return hits.length > 0 ? hits : closestCenter(args);
};

export function DashboardPage() {
  const { t } = useTranslation();
  const [metrics, setMetrics] = useState<Metrics | null>(null);
  const [connected, setConnected] = useState(false);
  const [editing, setEditing] = useState(false);
  const [activeId, setActiveId] = useState<string | null>(null);
  const [overId, setOverId] = useState<string | null>(null);
  const [resizingPanel, setResizingPanel] = useState(false);
  const { sets, targetedDomains, refresh: refreshSets } = useDashboardSets();
  const { order, hidden, spans, move, setSpan, setHidden, reset, customized } =
    useDashboardLayout();

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 8 } }),
  );

  useEffect(() => {
    let ws: WebSocket | null = null;
    let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
    let isCleaningUp = false;

    const connectWebSocket = () => {
      if (isCleaningUp) return;
      ws = new WebSocket(wsUrl("/api/ws/metrics"));

      ws.onopen = () => {
        setConnected(true);
      };

      ws.onmessage = (event) => {
        try {
          const data =
            typeof event.data === "string"
              ? (JSON.parse(event.data) as Metrics)
              : normalizeMetrics(null);
          setMetrics(normalizeMetrics(data));
        } catch {
          setMetrics(normalizeMetrics(null));
        }
      };

      ws.onerror = () => {
        setConnected(false);
      };

      ws.onclose = () => {
        setConnected(false);
        if (!isCleaningUp) {
          reconnectTimeout = setTimeout(connectWebSocket, 3000);
        }
      };
    };

    connectWebSocket();

    return () => {
      isCleaningUp = true;
      if (reconnectTimeout) clearTimeout(reconnectTimeout);
      if (ws) {
        ws.onopen = null;
        ws.onmessage = null;
        ws.onerror = null;
        ws.onclose = null;
        ws.close();
      }
    };
  }, []);

  const panelContext: PanelContext | null = useMemo(
    () => (metrics ? { metrics, sets, targetedDomains, refreshSets } : null),
    [metrics, sets, targetedDomains, refreshSets],
  );

  const visiblePanels = useMemo(() => {
    if (!panelContext) return [];
    return order
      .map((id) => PANELS_BY_ID.get(id))
      .filter((panel) => panel !== undefined)
      .filter((panel) => !hidden.has(panel.id))
      .filter((panel) => panel.available(panelContext))
      .map((panel) => ({
        id: panel.id,
        title: t(panel.titleKey),
        span: spans[panel.id] ?? panel.defaultSpan(panelContext),
        render: panel.render,
      }));
  }, [order, hidden, spans, panelContext, t]);

  const hiddenPanels: HiddenPanelEntry[] = useMemo(() => {
    if (!panelContext) return [];
    return order
      .filter((id) => hidden.has(id))
      .map((id) => PANELS_BY_ID.get(id))
      .filter((panel) => panel !== undefined)
      .map((panel) => ({
        id: panel.id,
        title: t(panel.titleKey),
        available: panel.available(panelContext),
      }));
  }, [order, hidden, panelContext, t]);

  if (!metrics || !panelContext) {
    return (
      <Container maxWidth={false} sx={{ py: 3 }}>
        <Box sx={{ textAlign: "center", py: 8 }}>
          <LinearProgress sx={{ mb: 2 }} />
          <Typography>{t("dashboard.loading")}</Typography>
        </Box>
      </Container>
    );
  }

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
    <Container maxWidth={false} sx={{ p: 2 }}>
      <HealthBanner
        metrics={metrics}
        connected={connected}
        editing={editing}
        onToggleEditing={() => setEditing((prev) => !prev)}
      />

      <CustomizeBar
        editing={editing}
        customized={customized}
        hiddenPanels={hiddenPanels}
        onShow={(id) => setHidden(id, false)}
        onReset={reset}
      />

      <DndContext
        sensors={sensors}
        collisionDetection={panelCollision}
        measuring={{ droppable: { strategy: MeasuringStrategy.Always } }}
        onDragStart={(event: DragStartEvent) =>
          setActiveId(String(event.active.id))
        }
        onDragOver={(event: DragOverEvent) =>
          setOverId(event.over ? String(event.over.id) : null)
        }
        onDragCancel={cancelDrag}
        onDragEnd={handleDragEnd}
      >
        <SortableContext
          items={visiblePanels.map((panel) => panel.id)}
          strategy={() => null}
        >
          <Box
            sx={{
              position: "relative",
              display: "grid",
              gridTemplateColumns: "repeat(12, minmax(0, 1fr))",
              gridAutoRows: `${ROW_UNIT}px`,
              gridAutoFlow: "row dense",
              alignItems: "start",
              columnGap: 1.5,
              rowGap: 0,
            }}
          >
            {resizingPanel && <ColumnGuides />}
            {visiblePanels.map((panel) => (
              <PanelFrame
                key={panel.id}
                id={panel.id}
                title={panel.title}
                span={panel.span}
                editing={editing}
                dropTarget={overId === panel.id && activeId !== panel.id}
                onSpanChange={(value) => setSpan(panel.id, value)}
                onResizeActive={setResizingPanel}
                onHide={() => setHidden(panel.id, true)}
              >
                {panel.render(panelContext)}
              </PanelFrame>
            ))}
          </Box>
        </SortableContext>

        <DragOverlay dropAnimation={null}>
          {activePanel ? <PanelGhost title={activePanel.title} /> : null}
        </DragOverlay>
      </DndContext>
    </Container>
  );
}
