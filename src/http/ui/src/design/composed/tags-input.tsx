"use client";

import * as React from "react";
import { useCallback, useRef, useState } from "react";

import {
  Combobox,
  ComboboxChip,
  ComboboxChips,
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
        {value.map((tag) => (
          <ComboboxChip key={tag}>{tag}</ComboboxChip>
        ))}
        <input
          ref={inputRef}
          type="text"
          value={inputValue}
          onChange={(e) => setInputValue(e.target.value)}
          onKeyDown={handleKeyDown}
          onBlur={() => inputValue && addTags(inputValue)}
          placeholder={value.length === 0 ? placeholder : ""}
          disabled={disabled}
          className="placeholder:text-muted-foreground min-w-16 flex-1 bg-transparent outline-none"
        />
      </ComboboxChips>
    </Combobox>
  );
}

export default TagsInput;
