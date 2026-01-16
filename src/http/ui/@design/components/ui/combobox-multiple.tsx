"use client";

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
} from "@design/components/ui/combobox";

export interface ComboboxMultipleProps {
  items: string[];
  value: string[];
  onValueChange: (values: string[]) => void;
  placeholder?: string;
  emptyMessage?: string;
  disabled?: boolean;
  breakdown?: Record<string, number>;
}

export function ComboboxMultiple({
  items,
  value,
  onValueChange,
  placeholder = "Search...",
  emptyMessage = "No items found.",
  disabled = false,
  breakdown,
}: ComboboxMultipleProps) {
  const anchor = useComboboxAnchor();

  return (
    <Combobox
      multiple
      autoHighlight
      items={items}
      value={value}
      onValueChange={onValueChange}
      disabled={disabled}
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
        <ComboboxList>
          {(item: string) => (
            <ComboboxItem key={item} value={item}>
              {item}
            </ComboboxItem>
          )}
        </ComboboxList>
      </ComboboxContent>
    </Combobox>
  );
}
