"use client";

import * as React from "react";
import { useCallback, useRef, useState } from "react";

import {
  Combobox,
  ComboboxChip,
  ComboboxChips,
  ComboboxChipsInput,
  ComboboxValue,
} from "@primitives/combobox";

export interface TagsInputProps {
  value: string[];
  onValueChange: (values: string[]) => void;
  placeholder?: string;
  disabled?: boolean;
  className?: string;
}

export function TagsInput({
  value,
  onValueChange,
  placeholder = "Type and press Enter...",
  disabled = false,
  className,
}: TagsInputProps) {
  const [inputValue, setInputValue] = useState("");
  const inputRef = useRef<HTMLInputElement>(null);

  const addTags = useCallback(
    (input: string) => {
      const tags = input
        .split(/[\s,|]+/)
        .map((t) => t.trim())
        .filter((t) => t && !value.includes(t));
      if (tags.length > 0) {
        onValueChange([...value, ...tags]);
      }
      setInputValue("");
    },
    [value, onValueChange],
  );

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" || e.key === "Tab" || e.key === ",") {
      e.preventDefault();
      addTags(inputValue);
    } else if (e.key === "Backspace" && !inputValue && value.length > 0) {
      onValueChange(value.slice(0, -1));
    }
  };

  return (
    <Combobox
      multiple
      value={value}
      onValueChange={onValueChange}
      items={value}
      disabled={disabled}
    >
      <ComboboxChips
        className={className}
        onClick={() => inputRef.current?.focus()}
      >
        <ComboboxValue>
          {(values: string[]) => (
            <React.Fragment>
              {values.map((tag) => (
                <ComboboxChip key={tag}>{tag}</ComboboxChip>
              ))}
              <ComboboxChipsInput
                ref={inputRef}
                value={inputValue}
                onChange={(e) => setInputValue(e.target.value)}
                onKeyDown={handleKeyDown}
                onBlur={() => inputValue && addTags(inputValue)}
                placeholder={values.length === 0 ? placeholder : ""}
                disabled={disabled}
              />
            </React.Fragment>
          )}
        </ComboboxValue>
      </ComboboxChips>
    </Combobox>
  );
}

export default TagsInput;
