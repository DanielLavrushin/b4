import { useEffect, useMemo, useRef, useState } from "react";
import type {
  ClipboardEvent,
  FocusEvent,
  KeyboardEvent,
  KeyboardEventHandler,
  SyntheticEvent,
} from "react";
import { Box, Chip, Tooltip, Typography } from "@mui/material";
import {
  DndContext,
  KeyboardSensor,
  MouseSensor,
  TouchSensor,
  closestCenter,
  useSensor,
  useSensors,
  type Announcements,
  type DragEndEvent,
  type UniqueIdentifier,
} from "@dnd-kit/core";
import {
  SortableContext,
  arrayMove,
  sortableKeyboardCoordinates,
  useSortable,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { useTranslation } from "react-i18next";
import { colors, fonts } from "@design";
import { resolverKey, resolverLabel, resolverValue } from "@utils";
import { useChangedFields } from "@context/ChangedFieldsContext";
import { B4AiExplain, aiHoverRevealSx } from "./B4AiExplain";
import { chipSx } from "./B4ChipList";
import { B4PlusButton } from "./B4PlusButton";
import { B4TextField } from "./B4TextField";

export interface B4ResolverInfo {
  name: string;
  desc?: string;
}

export interface B4ResolverPath {
  path: string;
  label: string;
}

interface B4ResolverInputProps {
  label: string;
  value: string[];
  onChange: (next: string[]) => void;
  validate: (entry: string) => string | null;
  chipProblem?: (entry: string, index: number) => string | null;
  highlight?: (entry: string) => boolean;
  max: number;
  paths?: B4ResolverPath[];
  lookup?: (entry: string) => B4ResolverInfo | undefined;
  placeholder?: string;
  helperText?: string;
  emptyMessage?: string;
  disabled?: boolean;
  aiTopic?: string;
}

interface ResolverChipProps {
  entry: string;
  index: number;
  problem: string | null;
  highlighted: boolean;
  info?: B4ResolverInfo;
  disabled?: boolean;
  onDelete: (byKey: boolean) => void;
}

interface FocusTarget {
  at: "input" | number;
}

const NO_PATHS: B4ResolverPath[] = [];
const SEVERAL_ENTRIES = /\S\s+\S/;

const keepInPlace = () => null;

const ResolverChip = ({
  entry,
  index,
  problem,
  highlighted,
  info,
  disabled,
  onDelete,
}: ResolverChipProps) => {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
    isSorting,
    isOver,
    activeIndex,
  } = useSortable({ id: entry, disabled });
  const { onKeyDown: pickUpByKey, ...pointerListeners } = listeners ?? {};
  const name = `${index + 1}. ${entry}`;
  const errorTone = problem
    ? {
        color: colors.state.error,
        border: `1px solid ${colors.state.error}`,
        "& .MuiChip-deleteIcon": { color: colors.state.error },
      }
    : undefined;
  let dropSide = 0;
  if (isOver && activeIndex !== index) dropSide = activeIndex > index ? -3 : 3;

  const details = (
    <Box sx={{ py: 0.25 }}>
      {info && (
        <Typography variant="subtitle2" component="div">
          {info.name}
        </Typography>
      )}
      <Typography
        variant="caption"
        component="div"
        sx={{ fontFamily: fonts.mono, wordBreak: "break-all" }}
      >
        {entry}
      </Typography>
      {info?.desc && (
        <Typography variant="caption" component="div">
          {info.desc}
        </Typography>
      )}
      {problem && (
        <Typography variant="caption" component="div" sx={{ fontWeight: 700 }}>
          {problem}
        </Typography>
      )}
    </Box>
  );

  return (
    <Tooltip title={isSorting ? "" : details} describeChild>
      <Chip
        ref={setNodeRef}
        {...attributes}
        {...pointerListeners}
        onKeyDownCapture={pickUpByKey as KeyboardEventHandler}
        aria-label={problem ? `${name}. ${problem}` : name}
        size="small"
        disabled={disabled}
        onDelete={(e: SyntheticEvent) => {
          if (!isDragging) onDelete(e.type === "keyup");
        }}
        style={{ transform: CSS.Translate.toString(transform), transition }}
        label={resolverLabel(entry)}
        sx={{
          ...chipSx(highlighted, false),
          ...errorTone,
          position: "relative",
          zIndex: isDragging ? 1 : "auto",
          maxWidth: "100%",
          cursor: isDragging ? "grabbing" : "grab",
          touchAction: "manipulation",
          opacity: isDragging ? 0.6 : 1,
          boxShadow: dropSide
            ? `${dropSide}px 0 0 ${colors.secondary}`
            : "none",
          "&:focus-visible": {
            outline: `2px solid ${colors.secondary}`,
            outlineOffset: 2,
          },
        }}
      />
    </Tooltip>
  );
};

export const B4ResolverInput = ({
  label,
  value,
  onChange,
  validate,
  chipProblem,
  highlight,
  max,
  paths = NO_PATHS,
  lookup,
  placeholder,
  helperText,
  emptyMessage,
  disabled,
  aiTopic,
}: B4ResolverInputProps) => {
  const { t } = useTranslation();
  const [input, setInput] = useState("");
  const [checked, setChecked] = useState(false);
  const [focused, setFocused] = useState(false);
  const [focusTarget, setFocusTarget] = useState<FocusTarget | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  const chipsRef = useRef<HTMLDivElement>(null);
  const { changedPaths, registerLabel, scopeVersion } = useChangedFields();
  useEffect(() => {
    for (const item of paths) registerLabel(item.path, item.label);
  }, [paths, registerLabel, scopeVersion]);
  const changed = paths.some((item) => changedPaths.has(item.path));

  useEffect(() => {
    if (!focusTarget) return;
    const chips =
      chipsRef.current?.querySelectorAll<HTMLElement>(".MuiChip-root");
    const last = chips?.[chips.length - 1];
    const field = inputRef.current;
    if (focusTarget.at === "input") (field?.disabled ? last : field)?.focus();
    else (chips?.[focusTarget.at] ?? last ?? field)?.focus();
  }, [focusTarget]);

  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 8 } }),
    useSensor(TouchSensor, {
      activationConstraint: { delay: 200, tolerance: 8 },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  const accessibility = useMemo(() => {
    const place = (moved: UniqueIdentifier, slot: UniqueIdentifier) => ({
      entry: String(moved),
      position: value.indexOf(String(slot)) + 1,
      total: value.length,
    });
    const notMoved = (moved: UniqueIdentifier) =>
      t("core.resolverInput.announce.cancel", { entry: String(moved) });
    const announcements: Announcements = {
      onDragStart: ({ active }) =>
        t("core.resolverInput.announce.start", place(active.id, active.id)),
      onDragOver: ({ active, over }) =>
        over && over.id !== active.id
          ? t("core.resolverInput.announce.over", place(active.id, over.id))
          : undefined,
      onDragEnd: ({ active, over }) =>
        over && over.id !== active.id
          ? t("core.resolverInput.announce.end", place(active.id, over.id))
          : notMoved(active.id),
      onDragCancel: ({ active }) => notMoved(active.id),
    };
    return {
      announcements,
      screenReaderInstructions: {
        draggable: t("core.resolverInput.announce.instructions"),
      },
    };
  }, [t, value]);

  const full = value.length >= max;

  const sort = (raw: string) => {
    const next = [...value];
    const keys = new Set(next.map(resolverKey));
    const kept: string[] = [];
    let reason: string | null = null;
    let unfit = 0;
    for (const entry of raw.split(/\s+/).filter(Boolean)) {
      const stored = resolverValue(entry);
      const key = resolverKey(stored);
      const problem =
        validate(entry) ??
        (keys.has(key) ? t("core.resolverInput.duplicate", { entry }) : null);
      if (problem) {
        kept.push(entry);
        reason ??= problem;
      } else if (next.length >= max) {
        kept.push(entry);
        unfit += 1;
      } else {
        keys.add(key);
        next.push(stored);
      }
    }
    return { next, kept, reason, unfit };
  };

  const add = (raw: string) => {
    const { next, kept } = sort(raw);
    if (next.length > value.length) onChange(next);
    setInput(kept.join(" "));
    setChecked(kept.length > 0);
  };

  const addAndStay = (raw: string) => {
    add(raw);
    setFocusTarget({ at: "input" });
  };

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key !== "Enter" || !input.trim()) return;
    e.preventDefault();
    addAndStay(input);
  };

  const handlePaste = (e: ClipboardEvent) => {
    const text = e.clipboardData.getData("text");
    const field = inputRef.current;
    if (!field || !SEVERAL_ENTRIES.test(text)) return;
    e.preventDefault();
    const start = field.selectionStart ?? field.value.length;
    const end = field.selectionEnd ?? start;
    addAndStay(field.value.slice(0, start) + text + field.value.slice(end));
  };

  const handleBlur = (e: FocusEvent<HTMLDivElement>) => {
    const next = e.relatedTarget;
    if (next instanceof Node && e.currentTarget.contains(next)) return;
    if (!document.hasFocus()) return;
    if (input.trim()) add(input);
  };

  const problemOf = (entry: string, index: number) => {
    if (chipProblem) return chipProblem(entry, index);
    return index >= max
      ? t("core.resolverInput.overLimit", { max })
      : validate(entry);
  };

  const handleDragEnd = ({ active, over }: DragEndEvent) => {
    if (!over || active.id === over.id) return;
    const from = value.indexOf(String(active.id));
    const to = value.indexOf(String(over.id));
    if (from >= 0 && to >= 0) onChange(arrayMove(value, from, to));
  };

  const pending = checked ? sort(input) : null;
  const reason = pending?.reason ?? null;
  const unfit = full ? (pending?.unfit ?? 0) : 0;
  let limitNote = full ? t("core.resolverInput.limit", { max }) : null;
  if (unfit > 0) {
    limitNote = t("core.resolverInput.notAdded", { count: unfit, max });
  }
  const notes = [reason, limitNote].filter(Boolean).join(" ");

  return (
    <Box onBlur={handleBlur}>
      <Box
        sx={{
          display: "flex",
          gap: 1,
          alignItems: "flex-start",
          "& > .MuiFormControl-root": { flex: 1, minWidth: 0 },
          ...aiHoverRevealSx,
        }}
      >
        <B4TextField
          label={label}
          value={input}
          inputRef={inputRef}
          changed={changed}
          error={!!reason || unfit > 0}
          disabled={disabled || (full && !input && !focused)}
          placeholder={placeholder}
          helperText={notes || helperText}
          onChange={(e) => {
            setInput(e.target.value);
            setChecked(false);
          }}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          onKeyDown={handleKeyDown}
          onPaste={handlePaste}
        />
        <B4PlusButton
          onClick={() => addAndStay(input)}
          disabled={disabled || full || !input.trim()}
        />
        {aiTopic && (
          <B4AiExplain
            topic={aiTopic}
            fieldLabel={label}
            fieldDoc={helperText}
            value={value.join(", ")}
          />
        )}
      </Box>
      {(value.length > 0 || emptyMessage) && (
        <Box
          ref={chipsRef}
          sx={{
            display: "flex",
            flexWrap: "wrap",
            gap: 1,
            p: 1,
            mt: 1,
            border: `1px solid ${colors.border.default}`,
            borderRadius: 1,
            bgcolor: colors.background.paper,
            minHeight: 40,
            alignItems: "center",
          }}
        >
          {value.length === 0 ? (
            <Typography variant="body2" color="text.secondary">
              {emptyMessage}
            </Typography>
          ) : (
            <DndContext
              sensors={sensors}
              collisionDetection={closestCenter}
              accessibility={accessibility}
              onDragEnd={handleDragEnd}
            >
              <SortableContext items={value} strategy={keepInPlace}>
                {value.map((entry, index) => (
                  <ResolverChip
                    key={entry}
                    entry={entry}
                    index={index}
                    problem={problemOf(entry, index)}
                    highlighted={highlight?.(entry) ?? false}
                    info={lookup?.(entry)}
                    disabled={disabled}
                    onDelete={(byKey) => {
                      onChange(value.filter((_, i) => i !== index));
                      if (byKey) setFocusTarget({ at: index });
                      else if (input.trim()) setFocusTarget({ at: "input" });
                    }}
                  />
                ))}
              </SortableContext>
            </DndContext>
          )}
        </Box>
      )}
    </Box>
  );
};
