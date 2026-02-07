import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import {
  AddIcon,
  CheckIcon,
  DomainIcon,
  IconSearch,
  SetsIcon,
  WarningIcon,
} from "@b4.icons";

import {
  DndContext,
  DragEndEvent,
  DragOverlay,
  DragStartEvent,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  SortableContext,
  rectSortingStrategy,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

import { useSnackbar } from "@context/SnackbarProvider";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@primitives/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@primitives/dialog";
import { Input } from "@primitives/input";
import { Separator } from "@primitives/separator";

import { SetCompare } from "./Compare";
import { SetCard } from "./SetCard";

import { cn } from "@design/lib/utils";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogMedia,
  AlertDialogTitle,
} from "@design/primitives/alert-dialog";
import { useSets } from "@hooks/useSets";
import { B4Config, B4SetConfig } from "@models/config";
import { Button } from "@primitives/button";

export interface SetStats {
  manual_domains: number;
  manual_ips: number;
  geosite_domains: number;
  geoip_ips: number;
  total_domains: number;
  total_ips: number;
  geosite_category_breakdown?: Record<string, number>;
  geoip_category_breakdown?: Record<string, number>;
}

export interface SetWithStats extends B4SetConfig {
  stats: SetStats;
}

interface SetsManagerProps {
  config: B4Config & { sets?: SetWithStats[] };
  onRefresh: () => void;
}

interface SortableCardWrapperProps {
  id: string;
  children:
    | React.ReactNode
    | ((props: React.HTMLAttributes<HTMLDivElement>) => JSX.Element);
}

const SortableCardWrapper = ({ id, children }: SortableCardWrapperProps) => {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id });

  return (
    <div
      ref={setNodeRef}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
        opacity: isDragging ? 0.4 : 1,
        zIndex: isDragging ? 1 : 0,
      }}
    >
      {/* Pass drag handle props to child */}
      {typeof children === "function"
        ? children({ ...attributes, ...listeners })
        : children}
    </div>
  );
};

export const SetsManager = ({ config, onRefresh }: SetsManagerProps) => {
  const { showSuccess, showError } = useSnackbar();
  const navigate = useNavigate();
  const { deleteSet, duplicateSet, reorderSets, updateSet } = useSets();

  const [filterText, setFilterText] = useState("");
  const [deleteDialog, setDeleteDialog] = useState<{
    open: boolean;
    setId: string | null;
  }>({
    open: false,
    setId: null,
  });
  const [compareDialog, setCompareDialog] = useState<{
    open: boolean;
    setA: B4SetConfig | null;
    setB: B4SetConfig | null;
  }>({ open: false, setA: null, setB: null });

  const [activeId, setActiveId] = useState<string | null>(null);

  const setsData = config.sets || [];
  const sets = setsData.map((s) => ("set" in s ? s.set : s)) as B4SetConfig[];
  const setsStats = setsData.map((s) =>
    "stats" in s ? s.stats : null,
  ) as (SetStats | null)[];

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 8 },
    }),
  );

  // Summary stats
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

  const filteredSets = useMemo(() => {
    if (!filterText.trim()) return sets;
    const lower = filterText.toLowerCase();
    return sets.filter((set) => {
      if (set.name.toLowerCase().includes(lower)) return true;
      if (
        set.targets?.sni_domains?.some((d) => d.toLowerCase().includes(lower))
      )
        return true;
      if (
        set.targets?.geosite_categories?.some((c) =>
          c.toLowerCase().includes(lower),
        )
      )
        return true;
      return false;
    });
  }, [sets, filterText]);

  const handleDragStart = (event: DragStartEvent) => {
    setActiveId(event.active.id as string);
  };

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    setActiveId(null);

    if (!over || active.id === over.id) return;

    const oldIndex = sets.findIndex((s) => s.id === active.id);
    const newIndex = sets.findIndex((s) => s.id === over.id);

    if (oldIndex === -1 || newIndex === -1) return;

    const newOrder = [...sets];
    const [removed] = newOrder.splice(oldIndex, 1);
    newOrder.splice(newIndex, 0, removed);

    void (async () => {
      const result = await reorderSets(newOrder.map((s) => s.id));
      if (result.success) onRefresh();
    })();
  };

  const activeSet = activeId ? sets.find((s) => s.id === activeId) : null;

  const handleAddSet = () => {
    navigate("/sets/new/targets");
  };

  const handleEditSet = (set: B4SetConfig) => {
    navigate(`/sets/${set.id}/targets`);
  };

  const handleDeleteSet = () => {
    if (!deleteDialog.setId) return;
    void (async () => {
      const result = await deleteSet(deleteDialog.setId!);
      if (result.success) {
        showSuccess("Set deleted");
        setDeleteDialog({ open: false, setId: null });
        onRefresh();
      } else {
        showError(result.error || "Failed to delete");
      }
    })();
  };

  const handleDuplicateSet = (set: B4SetConfig) => {
    void (async () => {
      const result = await duplicateSet(set);
      if (result.success) {
        showSuccess("Set duplicated");
        onRefresh();
      } else {
        showError(result.error || "Failed to duplicate");
      }
    })();
  };

  const handleToggleEnabled = (set: B4SetConfig, enabled: boolean) => {
    void (async () => {
      const updatedSet = { ...set, enabled };
      const result = await updateSet(updatedSet);
      if (result.success) {
        onRefresh();
      } else {
        showError(result.error || "Failed to update");
      }
    })();
  };

  return (
    <div className="flex flex-col gap-6">
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <SetsIcon />
            <CardTitle>Configuration Sets</CardTitle>
          </div>
          <CardDescription>
            Manage bypass configurations for different domains and scenarios
          </CardDescription>
        </CardHeader>
        <CardContent>
          {/* Summary Stats Bar */}
          <Card className="bg-muted border-border mb-6 border p-4">
            <div className="flex flex-row flex-wrap items-center justify-between gap-8">
              <div className="flex flex-row gap-8">
                <StatItem
                  value={summaryStats.total}
                  label="total sets"
                  color="text-foreground"
                />
                <StatItem
                  value={summaryStats.enabled}
                  label="enabled"
                  color="text-primary"
                  icon={<CheckIcon />}
                />
                <StatItem
                  value={summaryStats.totalDomains.toLocaleString()}
                  label="domains"
                  color="text-primary"
                  icon={<DomainIcon />}
                />
              </div>

              {/* Search & Add */}
              <div className="flex flex-row gap-4">
                <div className="relative w-50">
                  <IconSearch className="text-muted-foreground absolute top-1/2 left-3 size-5 -translate-y-1/2" />
                  <Input
                    placeholder="Search sets..."
                    value={filterText}
                    onChange={(e) => setFilterText(e.target.value)}
                    className="w-full pl-10"
                  />
                </div>
                <Button onClick={handleAddSet}>
                  <AddIcon className="mr-2 size-4" />
                  Create Set
                </Button>
              </div>
            </div>
          </Card>

          {/* Cards Grid */}
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragStart={handleDragStart}
            onDragEnd={handleDragEnd}
          >
            <SortableContext
              items={filteredSets.map((s) => s.id)}
              strategy={rectSortingStrategy}
            >
              <div className="flex flex-col gap-4">
                {filteredSets.map((set) => {
                  const index = sets.findIndex((s) => s.id === set.id);
                  const stats = setsStats[index] || undefined;

                  return (
                    <div key={set.id}>
                      <SortableCardWrapper id={set.id}>
                        {(
                          dragHandleProps: React.HTMLAttributes<HTMLDivElement>,
                        ) => (
                          <SetCard
                            set={set}
                            stats={stats}
                            index={index}
                            onEdit={() => handleEditSet(set)}
                            onDuplicate={() => handleDuplicateSet(set)}
                            onCompare={() =>
                              setCompareDialog({
                                open: true,
                                setA: set,
                                setB: null,
                              })
                            }
                            onDelete={() =>
                              setDeleteDialog({ open: true, setId: set.id })
                            }
                            onToggleEnabled={(enabled) =>
                              handleToggleEnabled(set, enabled)
                            }
                            dragHandleProps={dragHandleProps}
                          />
                        )}
                      </SortableCardWrapper>
                    </div>
                  );
                })}
              </div>
            </SortableContext>

            <DragOverlay>
              {activeSet ? (
                <div
                  className={cn(
                    "bg-card border-secondary min-w-70 border-2 p-6 shadow-lg",
                  )}
                >
                  <h6 className="text-lg font-semibold">{activeSet.name}</h6>
                  <p className="text-muted-foreground text-xs">
                    {activeSet.fragmentation.strategy.toUpperCase()}
                  </p>
                </div>
              ) : null}
            </DragOverlay>
          </DndContext>

          {/* Empty state */}
          {filteredSets.length === 0 && filterText && (
            <Card className="border-border border border-dashed p-8 text-center">
              <p className="text-muted-foreground">
                No sets match "{filterText}"
              </p>
            </Card>
          )}
        </CardContent>
      </Card>

      {/* Delete Confirmation */}
      <AlertDialog
        open={deleteDialog.open}
        onOpenChange={(open) =>
          !open && setDeleteDialog({ open: false, setId: null })
        }
      >
        <AlertDialogContent size="sm">
          <AlertDialogHeader>
            <AlertDialogMedia>
              <WarningIcon />
            </AlertDialogMedia>

            <AlertDialogTitle>Delete Configuration Set</AlertDialogTitle>

            <AlertDialogDescription>
              Are you sure you want to delete{" "}
              <strong>
                {sets.find((s) => s.id === deleteDialog.setId)?.name}
              </strong>
            </AlertDialogDescription>
          </AlertDialogHeader>

          <Separator />

          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction onClick={handleDeleteSet}>
              Delete Set
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Compare Selection Dialog */}
      <Dialog
        open={compareDialog.open && !compareDialog.setB}
        onOpenChange={(open) =>
          !open && setCompareDialog({ open: false, setA: null, setB: null })
        }
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Select Set to Compare</DialogTitle>
            <DialogDescription>
              Comparing with: {compareDialog.setA?.name}
            </DialogDescription>
          </DialogHeader>

          <Separator />

          <div className="flex flex-col gap-2">
            {sets
              .filter((s) => s.id !== compareDialog.setA?.id)
              .map((s) => (
                <div
                  key={s.id}
                  onClick={() =>
                    setCompareDialog((prev) => ({ ...prev, setB: s }))
                  }
                  className="hover:bg-accent cursor-pointer p-3 transition-colors"
                >
                  <p className="text-sm font-medium">{s.name}</p>
                </div>
              ))}
          </div>
        </DialogContent>
      </Dialog>

      <SetCompare
        open={compareDialog.open && !!compareDialog.setB}
        setA={compareDialog.setA}
        setB={compareDialog.setB}
        onClose={() =>
          setCompareDialog({ open: false, setA: null, setB: null })
        }
      />
    </div>
  );
};

interface StatItemProps {
  value: string | number;
  label: string;
  color: string;
  icon?: React.ReactNode;
}

const StatItem = ({ value, label, color, icon }: StatItemProps) => (
  <div className="flex flex-row items-center gap-2">
    {icon && <div className={cn("flex", color)}>{icon}</div>}
    <h5 className={cn("text-2xl font-bold", color)}>{value}</h5>
    <p className="text-muted-foreground text-sm">{label}</p>
  </div>
);
