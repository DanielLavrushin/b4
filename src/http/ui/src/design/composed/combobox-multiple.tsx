"use client";

import * as React from "react";
import { useVirtualizer, type Virtualizer } from "@tanstack/react-virtual";
import { Combobox as ComboboxPrimitive } from "@base-ui/react";

import {
  Combobox,
  ComboboxChip,
  ComboboxChips,
  ComboboxChipsInput,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxItem,
  ComboboxList,
  ComboboxValue,
  useComboboxAnchor,
} from "@primitives/combobox";
import { cn } from "@design/lib/utils";

export interface ComboboxMultipleProps {
  items: string[];
  value: string[];
  onValueChange: (values: string[]) => void;
  placeholder?: string;
  emptyMessage?: string;
  disabled?: boolean;
  loading?: boolean;
  breakdown?: Record<string, number>;
  /**
   * Включить виртуализацию для больших списков (рекомендуется для 200+ элементов).
   * По умолчанию: определяется автоматически на основе количества элементов (>150).
   */
  virtualized?: boolean;
}

export function ComboboxMultiple({
  items,
  value,
  onValueChange,
  placeholder = "Search...",
  emptyMessage = "No items found.",
  disabled = false,
  loading = false,
  breakdown,
  virtualized: virtualizedProp,
}: ComboboxMultipleProps) {
  const anchor = useComboboxAnchor();
  const itemsLoaded = items.length > 0;
  const [open, setOpen] = React.useState(false);
  const [searchValue, setSearchValue] = React.useState("");
  const virtualizerRef = React.useRef<Virtualizer<
    HTMLDivElement,
    HTMLDivElement
  > | null>(null);

  // Автоматически определяем, нужна ли виртуализация (для списков >150 элементов)
  // Если проп явно передан, используем его значение, иначе определяем автоматически
  const shouldVirtualize =
    virtualizedProp !== undefined ? virtualizedProp : items.length > 150;

  const deferredSearchValue = React.useDeferredValue(searchValue);

  const resolvedSearchValue =
    searchValue === "" || deferredSearchValue === ""
      ? searchValue
      : deferredSearchValue;

  const filteredItems = React.useMemo(() => {
    if (!itemsLoaded) return [];
    if (!resolvedSearchValue) return items;
    const searchLower = resolvedSearchValue.toLowerCase();
    return items.filter((item: string) =>
      item.toLowerCase().includes(searchLower),
    );
  }, [resolvedSearchValue, items, itemsLoaded]);

  const handleItemHighlighted = React.useCallback(
    (
      highlightedValue: string | undefined,
      eventDetails: { reason: string; index: number },
    ) => {
      if (!shouldVirtualize || !highlightedValue || !virtualizerRef.current) {
        return;
      }

      const { reason, index } = eventDetails;
      const isStart = index === 0;
      const isEnd = index === filteredItems.length - 1;
      const shouldScroll =
        reason === "none" || (reason === "keyboard" && (isStart || isEnd));

      if (shouldScroll) {
        queueMicrotask(() => {
          virtualizerRef.current?.scrollToIndex(index, {
            align: isEnd ? "start" : "end",
          });
        });
      }
    },
    [filteredItems.length, shouldVirtualize],
  );

  return (
    <Combobox
      multiple
      virtualized={shouldVirtualize}
      autoHighlight
      items={items}
      filteredItems={filteredItems}
      value={itemsLoaded ? value || [] : []}
      onValueChange={itemsLoaded ? onValueChange : () => {}}
      disabled={disabled || loading}
      open={open}
      onOpenChange={setOpen}
      inputValue={searchValue}
      onInputValueChange={setSearchValue}
      onItemHighlighted={shouldVirtualize ? handleItemHighlighted : undefined}
    >
      <ComboboxChips ref={anchor}>
        <ComboboxValue>
          {(values: string[]) => (
            <React.Fragment>
              {values.map((v) => (
                <ComboboxChip key={v}>
                  {v}
                  {breakdown?.[v] != null && (
                    <span className="text-muted-foreground ml-1">
                      ({breakdown[v]})
                    </span>
                  )}
                </ComboboxChip>
              ))}
              <ComboboxChipsInput placeholder={placeholder} />
            </React.Fragment>
          )}
        </ComboboxValue>
      </ComboboxChips>
      <ComboboxContent anchor={anchor}>
        <ComboboxEmpty>{emptyMessage}</ComboboxEmpty>
        {shouldVirtualize ? (
          <VirtualizedList
            items={filteredItems}
            renderItem={(item) => {
              return (
                <>
                  {item}
                  {breakdown?.[item] != null && (
                    <span className="text-muted-foreground ml-1">
                      ({breakdown[item]})
                    </span>
                  )}
                </>
              );
            }}
            itemKey={(item) => item}
            enabled={open}
            virtualizerRef={virtualizerRef}
          />
        ) : (
          <ComboboxList>
            {filteredItems.map((item) => (
              <ComboboxItem key={item} value={item}>
                {item}
                {breakdown?.[item] != null && (
                  <span className="text-muted-foreground ml-1">
                    ({breakdown[item]})
                  </span>
                )}
              </ComboboxItem>
            ))}
          </ComboboxList>
        )}
      </ComboboxContent>
    </Combobox>
  );
}

// Внутренний компонент виртуализированного списка
interface VirtualizedListProps<T> {
  items: T[];
  renderItem: (item: T) => React.ReactNode;
  itemKey: (item: T) => string | number;
  enabled?: boolean;
  virtualizerRef?: React.MutableRefObject<Virtualizer<
    HTMLDivElement,
    HTMLDivElement
  > | null>;
}

function VirtualizedList<T>({
  items,
  renderItem,
  itemKey,
  enabled = true,
  virtualizerRef,
}: VirtualizedListProps<T>) {
  const scrollElementRef = React.useRef<HTMLDivElement>(null);

  const virtualizer = useVirtualizer<HTMLDivElement, HTMLDivElement>({
    count: items.length,
    getScrollElement: () => scrollElementRef.current,
    estimateSize: () => 32,
    enabled,
    overscan: 20,
  });

  React.useEffect(() => {
    if (virtualizerRef) {
      virtualizerRef.current = virtualizer as Virtualizer<
        HTMLDivElement,
        HTMLDivElement
      >;
    }
  }, [virtualizer, virtualizerRef]);

  React.useEffect(() => {
    if (scrollElementRef.current) {
      virtualizer.measure();
    }
  }, [virtualizer]);

  const totalSize = virtualizer.getTotalSize();

  return (
    <ComboboxPrimitive.List
      data-slot="combobox-list"
      className={cn(
        "no-scrollbar max-h-[min(calc(--spacing(72)---spacing(9)),calc(var(--available-height)---spacing(9)))] scroll-py-1 p-1 data-empty:p-0",
      )}
    >
      {items.length > 0 && (
        <div
          role="presentation"
          ref={scrollElementRef}
          className="overflow-y-auto overscroll-contain"
          style={{
            maxHeight: "inherit",
          }}
        >
          <div
            role="presentation"
            className="relative w-full"
            style={{ height: totalSize }}
          >
            {virtualizer.getVirtualItems().map((virtualItem) => {
              const item = items[virtualItem.index];
              if (!item) {
                return null;
              }
              return (
                <ComboboxItem
                  key={itemKey(item)}
                  index={virtualItem.index}
                  data-index={virtualItem.index}
                  ref={virtualizer.measureElement}
                  value={String(itemKey(item))}
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    height: virtualItem.size,
                    transform: `translateY(${virtualItem.start}px)`,
                  }}
                >
                  {renderItem(item)}
                </ComboboxItem>
              );
            })}
          </div>
        </div>
      )}
    </ComboboxPrimitive.List>
  );
}
