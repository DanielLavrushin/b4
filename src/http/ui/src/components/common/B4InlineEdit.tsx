import { Button } from "@primitives/button";
import { Input } from "@primitives/input";
import { cn } from "@design/lib/utils";
import { CheckIcon, CloseIcon } from "@b4.icons";
import { useState } from "react";

interface B4InlineEditProps {
  value: string;
  onSave: (value: string) => Promise<void> | void;
  onCancel: () => void;
  disabled?: boolean;
  width?: number;
  className?: string;
}

export const B4InlineEdit = ({
  value: initialValue,
  onSave,
  onCancel,
  disabled = false,
  width = 150,
  className,
}: B4InlineEditProps) => {
  const [value, setValue] = useState(initialValue);
  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    if (!value.trim()) return;
    setSaving(true);
    try {
      await onSave(value.trim());
    } finally {
      setSaving(false);
    }
  };

  return (
    <div
      className={cn("flex items-center gap-1", className)}
      style={{ width: `${width}px` }}
    >
      <Input
        value={value}
        onChange={(e) => setValue(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter") void handleSave();
          else if (e.key === "Escape") onCancel();
        }}
        autoFocus
        disabled={saving || disabled}
        className="h-5"

      />
      <Button
        size="icon-sm"
        onClick={() => void handleSave()}
        disabled={saving || !value.trim()}
        variant="ghost"

      >
        <CheckIcon />
      </Button>
      <Button
        size="icon-sm"
        onClick={onCancel}
        disabled={saving}
        variant="ghost"
      >
        <CloseIcon />
      </Button>
    </div>
  );
};
