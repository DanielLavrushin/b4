import { cn } from "@design/lib/utils";
import { ExpandIcon, CollapseIcon } from "@b4.icons";

export type SortDirection = "asc" | "desc" | null;

interface SortableTableCellProps {
  label: string;
  active: boolean;
  direction: SortDirection;
  onSort: () => void;
  align?: "left" | "center" | "right";
}

export const SortableTableCell = ({
  label,
  active,
  direction,
  onSort,
  align = "left",
}: SortableTableCellProps) => {
  const alignClasses = {
    left: "text-left",
    center: "text-center",
    right: "text-right",
  };

  return (
    <th
      className={cn(
        "bg-card font-semibold border-b-2 border-border cursor-pointer select-none px-4 py-2 group",
        "hover:border-secondary hover:bg-muted/50 transition-colors",
        alignClasses[align]
      )}
      onClick={onSort}
    >
      <div className="flex items-center gap-2">
        <span
          className={cn(
            "transition-colors",
            active
              ? "text-primary"
              : "text-muted-foreground group-hover:text-foreground"
          )}
        >
          {label}
        </span>
        <div className="flex flex-col -space-y-1">
          <CollapseIcon
            className={cn(
              "h-3 w-3 transition-colors",
              active && direction === "asc"
                ? "text-primary"
                : "text-muted-foreground opacity-30 group-hover:opacity-60"
            )}
          />
          <ExpandIcon
            className={cn(
              "h-3 w-3 transition-colors",
              active && direction === "desc"
                ? "text-primary"
                : "text-muted-foreground opacity-30 group-hover:opacity-60"
            )}
          />
        </div>
      </div>
    </th>
  );
};
