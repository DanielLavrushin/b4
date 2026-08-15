import { useEffect, useRef, useState, type ReactNode } from "react";
import { Box, Typography } from "@mui/material";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useTranslation } from "react-i18next";
import { colors, radiusPx } from "@design";
import { DragIcon, HideIcon } from "@b4.icons";
import { B4TooltipButton } from "@common/B4TooltipButton";
import { GRID_COLUMNS } from "./registry";

const GRID_GAP = 12;
export const ROW_UNIT = 4;

interface PanelFrameProps {
  id: string;
  title: string;
  span: number;
  editing: boolean;
  dropTarget: boolean;
  onSpanChange: (span: number) => void;
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
  onHide,
  children,
}: PanelFrameProps) => {
  const { t } = useTranslation();
  const frameRef = useRef<HTMLDivElement | null>(null);
  const contentRef = useRef<HTMLDivElement | null>(null);
  const [resizing, setResizing] = useState(false);
  const [rowSpan, setRowSpan] = useState(1);

  useEffect(() => {
    const el = contentRef.current;
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
    transform,
    transition,
    isDragging,
  } = useSortable({ id, disabled: !editing });

  const handleResizeStart = (event: React.PointerEvent<HTMLDivElement>) => {
    const frame = frameRef.current;
    if (!frame) return;
    event.preventDefault();
    event.stopPropagation();

    const startX = event.clientX;
    const startSpan = span;
    const step = (frame.getBoundingClientRect().width + GRID_GAP) / startSpan;
    const grip = event.currentTarget;
    try {
      grip.setPointerCapture(event.pointerId);
    } catch {
      /* pointer already released */
    }
    setResizing(true);

    const onMove = (moveEvent: PointerEvent) => {
      onSpanChange(startSpan + (moveEvent.clientX - startX) / step);
    };
    const onEnd = () => {
      try {
        grip.releasePointerCapture(event.pointerId);
      } catch {
        /* pointer already released */
      }
      grip.removeEventListener("pointermove", onMove);
      grip.removeEventListener("pointerup", onEnd);
      grip.removeEventListener("pointercancel", onEnd);
      setResizing(false);
    };

    grip.addEventListener("pointermove", onMove);
    grip.addEventListener("pointerup", onEnd);
    grip.addEventListener("pointercancel", onEnd);
  };

  return (
    <Box
      ref={setNodeRef}
      sx={{
        gridColumn: { xs: "span 12", xl: `span ${span}` },
        gridRow: `span ${rowSpan}`,
        minWidth: 0,
        containerType: "inline-size",
      }}
      style={{
        transform: isDragging ? undefined : CSS.Translate.toString(transform),
        transition,
        zIndex: isDragging ? 1 : 0,
      }}
    >
      <Box ref={contentRef}>
      {editing ? (
        <Box
          ref={frameRef}
          sx={{
            position: "relative",
            p: "6px",
            pr: "12px",
            border: `1px dashed ${
              resizing || dropTarget ? colors.secondary : colors.border.strong
            }`,
            borderRadius: `${radiusPx.md}px`,
            bgcolor: dropTarget
              ? "rgba(245, 173, 24, 0.10)"
              : colors.background.control,
            opacity: isDragging ? 0.25 : 1,
          }}
        >
          <Box
            sx={{
              display: "flex",
              alignItems: "center",
              gap: "8px",
              mb: "6px",
              pl: "2px",
            }}
          >
            <Box
              {...attributes}
              {...listeners}
              sx={{
                display: "flex",
                alignItems: "center",
                gap: "6px",
                minWidth: 0,
                cursor: "grab",
                touchAction: "none",
                "&:active": { cursor: "grabbing" },
              }}
            >
              <DragIcon sx={{ fontSize: 18, color: colors.text.secondary }} />
              <Typography
                variant="metricLabel"
                sx={{
                  color: colors.text.secondary,
                  whiteSpace: "nowrap",
                  overflow: "hidden",
                  textOverflow: "ellipsis",
                }}
              >
                {title}
              </Typography>
            </Box>

            <Box sx={{ flex: 1 }} />

            <Typography
              variant="metricLabel"
              sx={{
                display: { xs: "none", xl: "block" },
                color: resizing ? colors.secondary : colors.text.secondary,
                opacity: resizing ? 1 : 0.7,
                fontFeatureSettings: '"tnum"',
                whiteSpace: "nowrap",
              }}
            >
              {t("dashboard.customize.columns", {
                span,
                total: GRID_COLUMNS,
              })}
            </Typography>

            <B4TooltipButton
              title={t("dashboard.customize.hide")}
              onClick={onHide}
              icon={<HideIcon sx={{ fontSize: 18 }} />}
            />
          </Box>

          <Box sx={{ pointerEvents: "none", userSelect: "none" }}>
            {children}
          </Box>

          <Box
            onPointerDown={handleResizeStart}
            sx={{
              display: { xs: "none", xl: "flex" },
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
        </Box>
      ) : (
        children
      )}
      </Box>
    </Box>
  );
};

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
      bgcolor: "rgba(245, 173, 24, 0.06)",
      cursor: "grabbing",
    }}
  >
    <DragIcon sx={{ fontSize: 18, color: colors.secondary }} />
    <Typography variant="metricLabel" sx={{ color: colors.secondary }}>
      {title}
    </Typography>
  </Box>
);
