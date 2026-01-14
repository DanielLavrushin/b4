import { Button } from "@design/components/ui/button";
import { Field, FieldLabel } from "@design/components/ui/field";
import { Input } from "@design/components/ui/input";
import { Label } from "@design/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@design/components/ui/select";
import { B4SetConfig, MAIN_SET_ID, NEW_SET_ID } from "@models/config";
import { AddIcon } from "@b4.icons";
import { useState } from "react";

interface SetSelectorProps {
  sets: B4SetConfig[];
  value: string;
  onChange: (setId: string, newSetName?: string) => void;
  label?: string;
  disabled?: boolean;
}

export const SetSelector = ({
  sets,
  value,
  onChange,
  label = "Target Set",
  disabled = false,
}: SetSelectorProps) => {
  const [isCreating, setIsCreating] = useState(false);
  const [newSetName, setNewSetName] = useState("");

  const handleCancelCreate = () => {
    setIsCreating(false);
    setNewSetName("");
  };

  if (isCreating) {
    return (
      <div className="w-full">
        <Field>
          <FieldLabel>Set Name</FieldLabel>
          <Input
            value={newSetName}
            onChange={(e) => {
              setNewSetName(e.target.value);
              onChange(NEW_SET_ID, e.target.value);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter" && newSetName.trim()) {
                setIsCreating(false);
                setNewSetName("");
              } else if (e.key === "Escape") {
                handleCancelCreate();
              }
            }}
            autoFocus
            className="w-full"
          />
        </Field>
        <div className="mt-2 flex justify-end">
          <Button
            size="sm"
            variant="ghost"
            onClick={() => {
              onChange(value || MAIN_SET_ID);
              handleCancelCreate();
            }}
          >
            Cancel
          </Button>
        </div>
      </div>
    );
  }

  return (
    <div className="w-full">
      <Label>{label}</Label>
      <Select
        value={value}
        onValueChange={(val) => {
          if (val === NEW_SET_ID) {
            setIsCreating(true);
          } else {
            onChange(val);
          }
        }}
        disabled={disabled}
      >
        <SelectTrigger className="w-full">
          <SelectValue placeholder={label} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem
            value={NEW_SET_ID}
            className="text-primary border-border border-b font-semibold"
          >
            <div className="flex items-center gap-2">
              <AddIcon />
              Create New Set
            </div>
          </SelectItem>
          {sets
            .filter((set) => set.enabled)
            .map((set) => (
              <SelectItem key={set.id} value={set.id}>
                {set.name}
              </SelectItem>
            ))}
        </SelectContent>
      </Select>
    </div>
  );
};
