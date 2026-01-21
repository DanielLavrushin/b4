"use client";

import * as React from "react";
import { type Virtualizer } from "@tanstack/react-virtual";

import {
  Combobox,
  ComboboxChip,
  ComboboxChips,
  ComboboxChipsInput,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxVirtualizedList,
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
  const virtualizerRef = React.useRef<Virtualizer<
    HTMLDivElement,
    HTMLDivElement
  > | null>(null);

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
      if (!highlightedValue || !virtualizerRef.current) {
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
    [filteredItems.length],
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
        <ComboboxVirtualizedList
          items={filteredItems}
          renderItem={(item) => item}
          itemKey={(item) => item}
          enabled={open}
          virtualizerRef={virtualizerRef}
        />
      </ComboboxContent>
    </Combobox>
  );
}
