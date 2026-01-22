"use client";

import { useVirtualizer } from "@tanstack/react-virtual";
import * as React from "react";

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

export interface ComboboxMultipleProps {
  items: string[];
  value: string[];
  onValueChange: (values: string[]) => void;
  placeholder?: string;
  emptyMessage?: string;
  disabled?: boolean;
  loading?: boolean;
  breakdown?: Record<string, number>;
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
}: ComboboxMultipleProps) {
  const anchor = useComboboxAnchor();
  const itemsLoaded = items.length > 0;
  const [open, setOpen] = React.useState(false);
  const [searchValue, setSearchValue] = React.useState("");
  const scrollElementRef = React.useRef<HTMLDivElement | null>(null);
  const [shouldRenderList, setShouldRenderList] = React.useState(false);

  const filteredItems = React.useMemo(() => {
    if (!itemsLoaded) return [];
    if (!searchValue) return items;
    const searchLower = searchValue.toLowerCase();
    return items.filter((item: string) =>
      item.toLowerCase().includes(searchLower),
    );
  }, [searchValue, items, itemsLoaded]);

  React.useEffect(() => {
    if (open) {
      const timeoutId = requestAnimationFrame(() => {
        setShouldRenderList(true);
      });
      return () => {
        cancelAnimationFrame(timeoutId);
      };
    } else {
      setShouldRenderList(false);
    }
  }, [open]);

  const virtualizer = useVirtualizer<HTMLDivElement, HTMLDivElement>({
    enabled: shouldRenderList,
    count: filteredItems.length,
    getScrollElement: () => scrollElementRef.current,
    estimateSize: () => 32,
    overscan: 20,
  });

  const handleScrollElementRef = React.useCallback(
    (element: HTMLDivElement | null) => {
      scrollElementRef.current = element;
      if (element && open) {
        virtualizer.measure();
      }
    },
    [virtualizer, open],
  );

  const handleItemHighlighted = React.useCallback(
    (
      highlightedValue: string | undefined,
      eventDetails: { reason: string; index: number },
    ) => {
      if (!highlightedValue) {
        return;
      }

      const { reason, index } = eventDetails;
      const isStart = index === 0;
      const isEnd = index === filteredItems.length - 1;
      const shouldScroll =
        reason === "none" || (reason === "keyboard" && (isStart || isEnd));

      if (shouldScroll) {
        queueMicrotask(() => {
          virtualizer.scrollToIndex(index, {
            align: isEnd ? "start" : "end",
          });
        });
      }
    },
    [filteredItems.length, virtualizer],
  );

  return (
    <Combobox
      multiple
      virtualized
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
      onItemHighlighted={handleItemHighlighted}
    >
      <div ref={anchor}>
        <ComboboxChips>
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
      </div>
      <ComboboxContent anchor={anchor}>
        <ComboboxEmpty>{emptyMessage}</ComboboxEmpty>
        {shouldRenderList ? (
          <ComboboxList>
            {filteredItems.length > 0 && (
              <div
                role="presentation"
                ref={handleScrollElementRef}
                className="overflow-y-auto overscroll-contain"
                style={{
                  maxHeight: "inherit",
                }}
              >
                <div
                  role="presentation"
                  className="relative w-full"
                  style={{ height: virtualizer.getTotalSize() }}
                >
                  {virtualizer.getVirtualItems().map((virtualItem) => {
                    const item = filteredItems[virtualItem.index];
                    if (!item) {
                      return null;
                    }
                    return (
                      <ComboboxItem
                        key={item}
                        index={virtualItem.index}
                        data-index={virtualItem.index}
                        ref={virtualizer.measureElement}
                        value={item}
                        style={{
                          position: "absolute",
                          top: 0,
                          left: 0,
                          width: "100%",
                          height: virtualItem.size,
                          transform: `translateY(${virtualItem.start}px)`,
                        }}
                      >
                        {item}
                        {breakdown?.[item] != null && (
                          <span className="text-muted-foreground ml-1">
                            ({breakdown[item]})
                          </span>
                        )}
                      </ComboboxItem>
                    );
                  })}
                </div>
              </div>
            )}
          </ComboboxList>
        ) : (
          <ComboboxList />
        )}
      </ComboboxContent>
    </Combobox>
  );
}
