import { useMemo, useState } from "react";

import {
  WarningIcon,
  CompareIcon,
  CheckIcon,
  AddIcon,
  IconSearch,
  SetsIcon,
  DomainIcon,
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
import { v4 as uuidv4 } from "uuid";

import { useSnackbar } from "@context/SnackbarProvider";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@design/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@design/components/ui/dialog";
import { Input } from "@design/components/ui/input";
import { Separator } from "@design/components/ui/separator";

import { SetCompare } from "./Compare";
import { SetEditor } from "./Editor";
import { SetCard } from "./SetCard";

import { Button } from "@design/components/ui/button";
import { cn } from "@design/lib/utils";
import { useSets } from "@hooks/useSets";
import { B4Config, B4SetConfig } from "@models/config";

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
  const {
    createSet,
    updateSet,
    deleteSet,
    duplicateSet,
    reorderSets,
    loading: saving,
  } = useSets();

  const [filterText, setFilterText] = useState("");
  const [editDialog, setEditDialog] = useState<{
    open: boolean;
    set: B4SetConfig | null;
    isNew: boolean;
  }>({
    open: false,
    set: null,
    isNew: false,
  });
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
    "stats" in s ? s.stats : null
  ) as (SetStats | null)[];

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: { distance: 8 },
    })
  );

  // Summary stats
  const summaryStats = useMemo(() => {
    const enabledCount = sets.filter((s) => s.enabled).length;
    const totalDomains = setsStats.reduce(
      (acc, s) => acc + (s?.total_domains || 0),
      0
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
          c.toLowerCase().includes(lower)
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
    const newSet: B4SetConfig = {
      id: uuidv4(),
      name: `Set ${sets.length + 1}`,
      enabled: true,
      tcp: {
        conn_bytes_limit: 19,
        seg2delay: 0,
        syn_fake: false,
        syn_fake_len: 0,
        syn_ttl: 3,
        drop_sack: false,
        win: { mode: "off", values: [0, 1460, 8192, 65535] },
        desync: { mode: "off", ttl: 3, count: 3, post_desync: false },
        incoming: {
          mode: "off",
          min: 14,
          max: 14,
          fake_ttl: 3,
          fake_count: 3,
          strategy: "badsum",
        },
      } as B4SetConfig["tcp"],
      udp: {
        mode: "fake",
        fake_seq_length: 6,
        fake_len: 64,
        faking_strategy: "none",
        dport_filter: "",
        filter_quic: "disabled",
        filter_stun: true,
        conn_bytes_limit: 8,
        seg2delay: 0,
      } as B4SetConfig["udp"],
      dns: {
        enabled: false,
        target_dns: "",
        fragment_query: false,
      } as B4SetConfig["dns"],
      fragmentation: {
        strategy: "tcp",
        reverse_order: true,
        middle_sni: true,
        sni_position: 1,
        oob_position: 0,
        oob_char: 120,
        tlsrec_pos: 0,
        seq_overlap: 0,
        seq_overlap_pattern: [],
        combo: {
          extension_split: true,
          first_byte_split: true,
          shuffle_mode: "middle",
          first_delay_ms: 100,
          jitter_max_us: 2000,
          decoy_enabled: false,
          decoy_snis: ["ya.ru", "vk.com", "mail.ru"],
        },
        disorder: {
          shuffle_mode: "full",
          min_jitter_us: 1000,
          max_jitter_us: 3000,
        },
      } as B4SetConfig["fragmentation"],
      faking: {
        sni: true,
        ttl: 8,
        strategy: "pastseq",
        seq_offset: 10000,
        sni_seq_length: 1,
        sni_type: 2,
        custom_payload: "",
        payload_file: "",
        tls_mod: [] as string[],
        sni_mutation: {
          mode: "off",
          grease_count: 3,
          padding_size: 2048,
          fake_ext_count: 5,
          fake_snis: ["ya.ru", "vk.com", "max.ru"],
        },
      } as B4SetConfig["faking"],
      targets: {
        sni_domains: [],
        ip: [],
        geosite_categories: [],
        geoip_categories: [],
      } as B4SetConfig["targets"],
    };

    setEditDialog({ open: true, set: newSet, isNew: true });
  };

  const handleEditSet = (set: B4SetConfig) => {
    setEditDialog({ open: true, set, isNew: false });
  };

  const handleSaveSet = (set: B4SetConfig) => {
    void (async () => {
      const result = editDialog.isNew
        ? await createSet(set)
        : await updateSet(set);

      if (result.success) {
        showSuccess(editDialog.isNew ? "Set created" : "Set updated");
        setEditDialog({ open: false, set: null, isNew: false });
        onRefresh();
      } else {
        showError(result.error || "Failed");
      }
    })();
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
          <Card className="p-4 mb-6 bg-muted border border-border rounded-md">
            <div className="flex flex-row gap-8 items-center justify-between flex-wrap">
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
                  <IconSearch className="absolute left-3 top-1/2 -translate-y-1/2 h-5 w-5 text-muted-foreground" />
                  <Input
                    placeholder="Search sets..."
                    value={filterText}
                    onChange={(e) => setFilterText(e.target.value)}
                    className="w-full pl-10"
                  />
                </div>
                <Button onClick={handleAddSet}>
                  <AddIcon className="h-4 w-4 mr-2" />
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
                          dragHandleProps: React.HTMLAttributes<HTMLDivElement>
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
                    "p-6 bg-card border-2 border-secondary rounded-md shadow-lg min-w-70"
                  )}
                >
                  <h6 className="text-lg font-semibold">{activeSet.name}</h6>
                  <p className="text-xs text-muted-foreground">
                    {activeSet.fragmentation.strategy.toUpperCase()}
                  </p>
                </div>
              ) : null}
            </DragOverlay>
          </DndContext>

          {/* Empty state */}
          {filteredSets.length === 0 && filterText && (
            <Card className="p-8 text-center border-dashed border border-border">
              <p className="text-muted-foreground">
                No sets match "{filterText}"
              </p>
            </Card>
          )}
        </CardContent>
      </Card>

      {/* Edit Dialog */}
      <SetEditor
        open={editDialog.open}
        settings={config.system}
        set={editDialog.set!}
        config={config}
        isNew={editDialog.isNew}
        saving={saving}
        stats={
          setsStats[sets.findIndex((s) => s.id === editDialog.set?.id)] ||
          undefined
        }
        onClose={() => setEditDialog({ open: false, set: null, isNew: false })}
        onSave={handleSaveSet}
      />

      {/* Delete Confirmation */}
      <Dialog
        open={deleteDialog.open}
        onOpenChange={(open) =>
          !open && setDeleteDialog({ open: false, setId: null })
        }
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
                <WarningIcon />
              </div>
              <div className="flex-1">
                <DialogTitle>Delete Configuration Set</DialogTitle>
                <DialogDescription className="mt-1">
                  This action cannot be undone
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>
          <div className="py-4">
            <p>
              Are you sure you want to delete{" "}
              <strong>
                {sets.find((s) => s.id === deleteDialog.setId)?.name}
              </strong>
              ?
            </p>
          </div>
          <Separator />
          <DialogFooter>
            <Button
              onClick={() => setDeleteDialog({ open: false, setId: null })}
              variant="outline"
            >
              Cancel
            </Button>
            <div className="flex-1" />
            <Button
              onClick={handleDeleteSet}
              className="bg-destructive hover:bg-destructive/90"
            >
              Delete Set
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Compare Selection Dialog */}
      <Dialog
        open={compareDialog.open && !compareDialog.setB}
        onOpenChange={(open) =>
          !open && setCompareDialog({ open: false, setA: null, setB: null })
        }
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <div className="flex items-center gap-3">
              <div className="flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
                <CompareIcon />
              </div>
              <div className="flex-1">
                <DialogTitle>Select Set to Compare</DialogTitle>
                <DialogDescription className="mt-1">
                  Comparing with: {compareDialog.setA?.name}
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>
          <div className="py-4">
            <div className="flex flex-col gap-2">
              {sets
                .filter((s) => s.id !== compareDialog.setA?.id)
                .map((s) => (
                  <div
                    key={s.id}
                    onClick={() =>
                      setCompareDialog((prev) => ({ ...prev, setB: s }))
                    }
                    className="cursor-pointer p-3 rounded-md hover:bg-accent transition-colors"
                  >
                    <p className="text-sm font-medium">{s.name}</p>
                  </div>
                ))}
            </div>
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
    <p className="text-sm text-muted-foreground">{label}</p>
  </div>
);
