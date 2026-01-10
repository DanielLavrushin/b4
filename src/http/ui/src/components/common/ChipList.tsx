import * as React from "react";
import { Badge } from "@design/components/ui/badge";
import { CloseIcon } from "@b4.icons";
import { cn } from "@design/lib/utils";

interface ChipListProps<T> {
  items: T[];
  getKey: (item: T) => string | number;
  getLabel: (item: T) => React.ReactNode;
  onDelete?: (item: T) => void;
  onClick?: (item: T) => void;
  title?: string;
  emptyMessage?: string;
  showEmpty?: boolean;
  maxHeight?: number;
  className?: string;
}

export function ChipList<T>({
  items,
  getKey,
  getLabel,
  onDelete,
  onClick,
  title,
  emptyMessage = "No items",
  maxHeight,
  showEmpty = false,
  className,
}: ChipListProps<T>) {
  if (items.length === 0 && !showEmpty) return null;

  const content = (
    <div className={cn("space-y-2", className)}>
      {title && <h3 className="text-sm font-medium">{title}</h3>}
      <div
        className={cn(
          "flex flex-wrap gap-2 p-2 border border-dashed rounded-md bg-card min-h-10 items-center",
          maxHeight && "overflow-y-auto"
        )}
        style={maxHeight ? { maxHeight: `${maxHeight}px` } : undefined}
      >
        {items.length === 0 ? (
          <p className="text-sm text-muted-foreground">{emptyMessage}</p>
        ) : (
          items.map((item) => (
            <Badge
              key={getKey(item)}
              variant="secondary"
              className={cn(
                "flex items-center gap-1",
                onClick && "cursor-pointer hover:bg-secondary/80"
              )}
              onClick={onClick ? () => onClick(item) : undefined}
            >
              {getLabel(item)}
              {onDelete && (
                <button
                  onClick={(e) => {
                    e.stopPropagation();
                    onDelete(item);
                  }}
                  className="ml-1 rounded-full hover:bg-secondary-foreground/20 p-0.5"
                >
                  <CloseIcon className="h-3 w-3" />
                </button>
              )}
            </Badge>
          ))
        )}
      </div>
    </div>
  );

  if (items.length === 0) {
    return null;
  }

  return content;
}
