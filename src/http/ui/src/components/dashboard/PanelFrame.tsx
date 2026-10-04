import { useEffect, useRef, useState, type ReactNode } from "react";
import { Box, IconButton, Tooltip } from "@mui/material";
import { Add as WiderIcon, Remove as NarrowerIcon } from "@mui/icons-material";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useTranslation } from "react-i18next";
import { colors, radiusPx } from "@design";
import { DragIcon, HideIcon } from "@b4.icons";
import { GRID_COLUMNS, MIN_SPAN } from "./registry";

export const GRID_GAP = 12;
export const ROW_UNIT = 4;
export const GRID_CONTAINER = "dashgrid";
export const WIDE_GRID = `@container ${GRID_CONTAINER} (min-width: 960px)`;

const clampSpan = (value: number): number =>
  Math.min(GRID_COLUMNS, Math.max(MIN_SPAN, Math.round(value)));

const editLabelSx = {
  fontSize: 11,
  fontWeight: 600,
  letterSpacing: "0.1em",
  textTransform: "uppercase",
  lineHeight: 1.2,
  color: colors.text.secondary,
} as const;

const wideOnly = {
  display: "none",
  [WIDE_GRID]: { display: "flex" },
} as const;

interface PanelFrameProps {
  id: string;
  title: string;
  span: number;
  editing: boolean;
  dropTarget: boolean;
  onSpanChange: (span: number) => void;
  onResizeActive: (active: boolean) => void;
  onHide: () => void;
  children: ReactNode;
}

export const PanelFrame = ({
  id,
  title,
  span,
  editing,
  dropTarget,
  onSpanChange,
  onResizeActive,
  onHide,
  children,
}: PanelFrameProps) => {
  const { t } = useTranslation();
  const frameRef = useRef<HTMLDivElement | null>(null);
  const [resizing, setResizing] = useState(false);
  const [previewSpan, setPreviewSpan] = useState<number | null>(null);
  const [rowSpan, setRowSpan] = useState(1);
  const shownSpan = previewSpan ?? span;

  useEffect(() => {
    const el = frameRef.current;
    if (!el) return;
    const observer = new ResizeObserver(() => {
      const height = el.getBoundingClientRect().height;
      const rows = Math.max(1, Math.ceil((height + GRID_GAP) / ROW_UNIT));
      setRowSpan((prev) => (prev === rows ? prev : rows));
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  const {
    attributes,
    listeners,
    setNodeRef,
    setActivatorNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({
    id,
    disabled: !editing,
    attributes: { roleDescription: t("dashboard.customize.roleDescription") },
  });

  const handleResizeStart = (event: React.PointerEvent<HTMLDivElement>) => {
    const frame = frameRef.current;
    if (!frame) return;
    event.preventDefault();
    event.stopPropagation();

    const startX = event.clientX;
    const startSpan = span;
    const step = (frame.getBoundingClientRect().width + GRID_GAP) / startSpan;
    const grip = event.currentTarget;
    const pointerId = event.pointerId;
    const capture = (on: boolean) => {
      try {
        if (on) grip.setPointerCapture(pointerId);
        else grip.releasePointerCapture(pointerId);
      } catch {
        return;
      }
    };
    capture(true);
    setResizing(true);
    onResizeActive(true);

    let latest = startSpan;
    const onMove = (moveEvent: PointerEvent) => {
      latest = clampSpan(startSpan + (moveEvent.clientX - startX) / step);
      setPreviewSpan(latest);
    };
    const onEnd = () => {
      capture(false);
      grip.removeEventListener("pointermove", onMove);
      grip.removeEventListener("pointerup", onEnd);
      grip.removeEventListener("pointercancel", onEnd);
      setResizing(false);
      onResizeActive(false);
      setPreviewSpan(null);
      if (latest !== startSpan) onSpanChange(latest);
    };

    grip.addEventListener("pointermove", onMove);
    grip.addEventListener("pointerup", onEnd);
    grip.addEventListener("pointercancel", onEnd);
  };

  const highlight = resizing || dropTarget;

  return (
    <Box
      ref={setNodeRef}
      data-panel-id={id}
      sx={{
        gridColumn: "span 12",
        gridRow: `span ${rowSpan}`,
        minWidth: 0,
        [WIDE_GRID]: { gridColumn: `span ${shownSpan}` },
      }}
      style={{
        transform: isDragging ? undefined : CSS.Translate.toString(transform),
        transition,
        zIndex: isDragging ? 1 : 0,
      }}
    >
      <Box
        ref={frameRef}
        sx={
          editing
            ? {
                position: "relative",
                p: "6px",
                pr: "12px",
                border: `1px dashed ${highlight ? colors.secondary : colors.border.strong}`,
                borderRadius: `${radiusPx.md}px`,
                bgcolor: dropTarget ? colors.accent.secondaryHover : colors.background.control,
                opacity: isDragging ? 0.25 : 1,
              }
            : { position: "relative" }
        }
      >
        {editing ? (
          <Box
            sx={{
              display: "flex",
              alignItems: "center",
              gap: "6px",
              mb: "6px",
              pl: "2px",
              minHeight: 30,
            }}
          >
            <Box
              component="button"
              type="button"
              ref={setActivatorNodeRef}
              {...attributes}
              {...listeners}
              aria-label={t("dashboard.customize.move", { title })}
              sx={{
                display: "flex",
                alignItems: "center",
                gap: "6px",
                minWidth: 0,
                p: "4px 6px",
                ml: "-4px",
                border: 0,
                borderRadius: `${radiusPx.sm}px`,
                bgcolor: "transparent",
                color: "inherit",
                font: "inherit",
                cursor: "grab",
                touchAction: "none",
                "&:active": { cursor: "grabbing" },
                "&:focus-visible": {
                  outline: `2px solid ${colors.secondary}`,
                  outlineOffset: "1px",
                },
              }}
            >
              <DragIcon sx={{ fontSize: 18, color: colors.text.secondary }} />
              <Box
                component="span"
                sx={{
                  ...editLabelSx,
                  whiteSpace: "nowrap",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                }}
              >
                {title}
              </Box>
            </Box>

            <Box sx={{ flex: 1 }} />

            <Box sx={{ ...wideOnly, alignItems: "center", gap: "2px" }}>
              <Tooltip title={t("dashboard.customize.narrower")}>
                <span>
                  <IconButton
                    size="small"
                    aria-label={t("dashboard.customize.narrowerPanel", { title })}
                    disabled={shownSpan <= MIN_SPAN}
                    onClick={() => onSpanChange(clampSpan(span - 1))}
                    sx={{ color: colors.text.secondary, p: "4px" }}
                  >
                    <NarrowerIcon sx={{ fontSize: 16 }} />
                  </IconButton>
                </span>
              </Tooltip>
              <Box
                component="span"
                aria-live="polite"
                sx={{
                  ...editLabelSx,
                  color: resizing ? colors.secondary : colors.text.secondary,
                  fontVariantNumeric: "tabular-nums",
                  whiteSpace: "nowrap",
                  minWidth: 64,
                  textAlign: "center",
                }}
              >
                {t("dashboard.customize.columns", {
                  span: shownSpan,
                  total: GRID_COLUMNS,
                })}
              </Box>
              <Tooltip title={t("dashboard.customize.wider")}>
                <span>
                  <IconButton
                    size="small"
                    aria-label={t("dashboard.customize.widerPanel", { title })}
                    disabled={shownSpan >= GRID_COLUMNS}
                    onClick={() => onSpanChange(clampSpan(span + 1))}
                    sx={{ color: colors.text.secondary, p: "4px" }}
                  >
                    <WiderIcon sx={{ fontSize: 16 }} />
                  </IconButton>
                </span>
              </Tooltip>
            </Box>

            <Tooltip title={t("dashboard.customize.hide")}>
              <IconButton
                size="small"
                aria-label={t("dashboard.customize.hidePanel", { title })}
                onClick={onHide}
                sx={{ color: colors.text.secondary, p: "4px" }}
              >
                <HideIcon sx={{ fontSize: 18 }} />
              </IconButton>
            </Tooltip>
          </Box>
        ) : null}

        <Box
          inert={editing}
          sx={
            editing ? { pointerEvents: "none", userSelect: "none" } : undefined
          }
        >
          {children}
        </Box>

        {editing ? (
          <Box
            aria-hidden
            onPointerDown={handleResizeStart}
            sx={{
              ...wideOnly,
              position: "absolute",
              top: "4px",
              right: 0,
              bottom: "4px",
              width: "12px",
              alignItems: "center",
              justifyContent: "center",
              cursor: "col-resize",
              touchAction: "none",
              "&::after": {
                content: '""',
                width: "3px",
                height: "100%",
                maxHeight: "48px",
                borderRadius: "2px",
                bgcolor: resizing ? colors.secondary : colors.border.strong,
              },
              "&:hover::after": { bgcolor: colors.secondary },
            }}
          />
        ) : null}
      </Box>
    </Box>
  );
};

export const ColumnGuides = () => (
  <Box
    aria-hidden
    sx={{
      display: "none",
      position: "absolute",
      inset: 0,
      gridTemplateColumns: `repeat(${GRID_COLUMNS}, minmax(0, 1fr))`,
      columnGap: `${GRID_GAP}px`,
      pointerEvents: "none",
      zIndex: 2,
      [WIDE_GRID]: { display: "grid" },
    }}
  >
    {Array.from({ length: GRID_COLUMNS }, (_, i) => (
      <Box
        key={i}
        sx={{
          bgcolor: colors.accent.secondaryHover,
          border: `1px solid ${colors.border.light}`,
          borderRadius: "2px",
        }}
      />
    ))}
  </Box>
);

export const PanelGhost = ({ title }: { title: string }) => (
  <Box
    sx={{
      height: "100%",
      maxHeight: "96px",
      display: "flex",
      alignItems: "flex-start",
      gap: "6px",
      p: "8px 10px",
      border: `2px dashed ${colors.secondary}`,
      borderRadius: `${radiusPx.md}px`,
      bgcolor: colors.accent.secondaryHover,
      cursor: "grabbing",
    }}
  >
    <DragIcon sx={{ fontSize: 18, color: colors.secondary }} />
    <Box component="span" sx={{ ...editLabelSx, color: colors.secondary }}>
      {title}
    </Box>
  </Box>
);
