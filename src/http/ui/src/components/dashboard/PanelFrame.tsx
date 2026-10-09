import { useState, type ReactNode } from "react";
import { Box, IconButton, Tooltip } from "@mui/material";
import {
  Add as WiderIcon,
  KeyboardReturn as NewRowIcon,
  MoveUp as JoinRowIcon,
  Remove as NarrowerIcon,
} from "@mui/icons-material";
import { useDroppable } from "@dnd-kit/core";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useTranslation } from "react-i18next";
import { colors, radiusPx } from "@design";
import { DragIcon, HideIcon } from "@b4.icons";
import type { DropSide, RowBreak } from "./layoutRows";
import { GRID_COLUMNS } from "./registry";

export const GRID_GAP = 12;
export const EDIT_ROW_GAP = 20;
export const GRID_CONTAINER = "dashgrid";
export const WIDE_GRID = `@container ${GRID_CONTAINER} (min-width: 960px)`;
export const GAP_PREFIX = "row-gap:";

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

const dropBarSx = (side: DropSide) =>
  ({
    position: "absolute",
    zIndex: 3,
    borderRadius: "2px",
    bgcolor: colors.secondary,
    pointerEvents: "none",
    left: 0,
    right: 0,
    height: "3px",
    ...(side === "before" ? { top: "-8px" } : { bottom: "-8px" }),
    [WIDE_GRID]: {
      top: 0,
      bottom: 0,
      height: "auto",
      width: "3px",
      left: side === "before" ? "-8px" : "auto",
      right: side === "after" ? "-8px" : "auto",
    },
  }) as const;

interface PanelFrameProps {
  id: string;
  title: string;
  span: number;
  row: number;
  editing: boolean;
  dropSide: DropSide | null;
  resizable: boolean;
  canGrow: boolean;
  canShrink: boolean;
  rowBreak: { kind: RowBreak; allowed: boolean };
  onGrow: () => void;
  onShrink: () => void;
  onResizeMove: (delta: number) => void;
  onResizeEnd: (delta: number) => void;
  onToggleRowBreak: () => void;
  onHide: () => void;
  children: ReactNode;
}

export const PanelFrame = ({
  id,
  title,
  span,
  row,
  editing,
  dropSide,
  resizable,
  canGrow,
  canShrink,
  rowBreak,
  onGrow,
  onShrink,
  onResizeMove,
  onResizeEnd,
  onToggleRowBreak,
  onHide,
  children,
}: PanelFrameProps) => {
  const { t } = useTranslation();
  const [resizing, setResizing] = useState(false);

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
    const frame = event.currentTarget.parentElement;
    if (!frame) return;
    event.preventDefault();
    event.stopPropagation();

    const startX = event.clientX;
    const step = (frame.getBoundingClientRect().width + GRID_GAP) / span;
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
    onResizeMove(0);

    let latest = 0;
    const onMove = (moveEvent: PointerEvent) => {
      const delta = Math.round((moveEvent.clientX - startX) / step);
      if (delta === latest) return;
      latest = delta;
      onResizeMove(delta);
    };
    const onEnd = () => {
      capture(false);
      grip.removeEventListener("pointermove", onMove);
      grip.removeEventListener("pointerup", onEnd);
      grip.removeEventListener("pointercancel", onEnd);
      setResizing(false);
      onResizeEnd(latest);
    };

    grip.addEventListener("pointermove", onMove);
    grip.addEventListener("pointerup", onEnd);
    grip.addEventListener("pointercancel", onEnd);
  };

  const highlight = resizing || dropSide !== null;
  const joinsRow = rowBreak.kind === "join";

  return (
    <Box
      ref={setNodeRef}
      data-panel-id={id}
      sx={{
        display: "flex",
        flexDirection: "column",
        minWidth: 0,
        gridColumn: "span 12",
        [WIDE_GRID]: { gridColumn: `span ${span}`, gridRow: row },
      }}
      style={{
        transform: isDragging ? undefined : CSS.Translate.toString(transform),
        transition,
        zIndex: isDragging ? 1 : 0,
      }}
    >
      <Box
        sx={{
          position: "relative",
          flex: "1 1 auto",
          display: "flex",
          flexDirection: "column",
          ...(editing
            ? {
                p: "6px",
                pr: "12px",
                border: `1px dashed ${highlight ? colors.secondary : colors.border.strong}`,
                borderRadius: `${radiusPx.md}px`,
                bgcolor: colors.background.control,
                opacity: isDragging ? 0.25 : 1,
              }
            : {}),
        }}
      >
        {dropSide && <Box aria-hidden sx={dropBarSx(dropSide)} />}
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
                    disabled={!canShrink}
                    onClick={onShrink}
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
                {t("dashboard.customize.columns", { span, total: GRID_COLUMNS })}
              </Box>
              <Tooltip title={t("dashboard.customize.wider")}>
                <span>
                  <IconButton
                    size="small"
                    aria-label={t("dashboard.customize.widerPanel", { title })}
                    disabled={!canGrow}
                    onClick={onGrow}
                    sx={{ color: colors.text.secondary, p: "4px" }}
                  >
                    <WiderIcon sx={{ fontSize: 16 }} />
                  </IconButton>
                </span>
              </Tooltip>
              {rowBreak.kind && (
                <Tooltip
                  title={
                    joinsRow
                      ? rowBreak.allowed
                        ? t("dashboard.customize.joinRow")
                        : t("dashboard.customize.rowFull")
                      : t("dashboard.customize.newRow")
                  }
                >
                  <span>
                    <IconButton
                      size="small"
                      aria-label={
                        joinsRow
                          ? t("dashboard.customize.joinRowPanel", { title })
                          : t("dashboard.customize.newRowPanel", { title })
                      }
                      disabled={!rowBreak.allowed}
                      onClick={onToggleRowBreak}
                      sx={{ color: colors.text.secondary, p: "4px" }}
                    >
                      {joinsRow ? (
                        <JoinRowIcon sx={{ fontSize: 16 }} />
                      ) : (
                        <NewRowIcon sx={{ fontSize: 16 }} />
                      )}
                    </IconButton>
                  </span>
                </Tooltip>
              )}
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
          sx={{
            flex: "1 1 auto",
            display: "flex",
            flexDirection: "column",
            ...(editing ? { pointerEvents: "none", userSelect: "none" } : {}),
          }}
        >
          {children}
        </Box>

        {editing && resizable ? (
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

export const RowGap = ({
  id,
  row,
  dragging,
}: {
  id: string;
  row: number;
  dragging: boolean;
}) => {
  const { t } = useTranslation();
  const { setNodeRef, isOver } = useDroppable({ id });
  return (
    <Box
      ref={setNodeRef}
      aria-hidden
      sx={{
        display: "none",
        [WIDE_GRID]: {
          display: "flex",
          gridColumn: "1 / -1",
          gridRow: row,
          height: `${EDIT_ROW_GAP}px`,
          alignItems: "center",
          gap: "8px",
        },
      }}
    >
      {dragging && (
        <>
          <Box
            sx={{
              flex: 1,
              height: isOver ? "3px" : "1px",
              borderRadius: "2px",
              bgcolor: isOver ? colors.secondary : colors.border.default,
            }}
          />
          {isOver && (
            <Box component="span" sx={{ ...editLabelSx, color: colors.secondary }}>
              {t("dashboard.customize.newRowHere")}
            </Box>
          )}
        </>
      )}
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
