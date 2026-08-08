// src/http/ui/src/components/common/B4ChipList.tsx
import { useState } from "react";
import { Box, Chip, Grid, Typography } from "@mui/material";
import { colors } from "@design";

interface B4ChipListProps<T> {
  items: T[];
  getKey: (item: T) => string | number;
  getLabel: (item: T) => React.ReactNode;
  onDelete?: (item: T) => void;
  onClick?: (item: T) => void;
  title?: string;
  emptyMessage?: string;
  gridSize?: { xs?: number; sm?: number; md?: number; lg?: number };
  showEmpty?: boolean;
  maxHeight?: number;
  collapsedMax?: number;
  highlight?: (item: T) => boolean;
}

const chipSx = (accented: boolean, clickable: boolean) => ({
  bgcolor: accented ? colors.accent.secondary : colors.accent.primary,
  color: accented ? colors.state.warning : colors.secondary,
  fontWeight: accented ? 600 : undefined,
  border: accented ? `1px solid ${colors.state.warning}` : undefined,
  cursor: clickable ? "pointer" : undefined,
  "& .MuiChip-deleteIcon": {
    color: accented ? colors.state.warning : colors.secondary,
  },
});

export function B4ChipList<T>({
  items,
  getKey,
  getLabel,
  onDelete,
  onClick,
  title,
  emptyMessage = "No items",
  gridSize,
  maxHeight,
  showEmpty = false,
  collapsedMax,
  highlight,
}: Readonly<B4ChipListProps<T>>) {
  const [expanded, setExpanded] = useState(false);

  if (items.length === 0 && !showEmpty) return null;

  const canCollapse = collapsedMax != null && items.length > collapsedMax;
  const visibleItems = canCollapse && !expanded ? items.slice(0, collapsedMax) : items;
  const hiddenCount = items.length - (collapsedMax ?? 0);

  const content = (
    <>
      {title && (
        <Typography variant="subtitle2" gutterBottom>
          {title}
        </Typography>
      )}
      <Box
        sx={{
          display: "flex",
          flexWrap: "wrap",
          gap: 1,
          p: 1,
          border: `1px solid ${colors.border.default}`,
          borderRadius: 1,
          bgcolor: colors.background.paper,
          minHeight: 40,
          alignItems: "center",
          ...(maxHeight && { maxHeight, overflowY: "auto" }),
        }}
      >
        {items.length === 0 ? (
          <Typography variant="body2" color="text.secondary">
            {emptyMessage}
          </Typography>
        ) : (
          <>
            {visibleItems.map((item) => (
              <Chip
                key={getKey(item)}
                label={getLabel(item)}
                onDelete={onDelete ? () => onDelete(item) : undefined}
                onClick={onClick ? () => onClick(item) : undefined}
                size="small"
                sx={chipSx(highlight?.(item) ?? false, !!onClick)}
              />
            ))}
            {canCollapse && !expanded && (
              <Chip
                label={`+${hiddenCount} more`}
                size="small"
                onClick={() => setExpanded(true)}
                sx={{
                  bgcolor: colors.background.dark,
                  color: colors.text.secondary,
                  cursor: "pointer",
                  fontWeight: 600,
                }}
              />
            )}
            {canCollapse && expanded && (
              <Chip
                label="Show less"
                size="small"
                onClick={() => setExpanded(false)}
                sx={{
                  bgcolor: colors.background.dark,
                  color: colors.text.secondary,
                  cursor: "pointer",
                  fontWeight: 600,
                }}
              />
            )}
          </>
        )}
      </Box>
    </>
  );
  if (items.length === 0) {
    return <></>;
  }

  if (gridSize) {
    return <Grid size={gridSize}>{content}</Grid>;
  }

  return content;
}
