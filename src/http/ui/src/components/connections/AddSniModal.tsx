import { AddIcon, DomainIcon } from "@b4.icons";
import { SetSelector } from "@common/SetSelector";
import { B4SetConfig, MAIN_SET_ID, NEW_SET_ID } from "@models/config";
import { Alert, AlertDescription } from "@primitives/alert";
import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@primitives/dialog";
import {
  Field,
  FieldContent,
  FieldDescription,
  FieldTitle,
} from "@primitives/field";
import { Label } from "@primitives/label";
import { RadioGroup, RadioGroupItem } from "@primitives/radio-group";
import { Separator } from "@primitives/separator";
import { useEffect, useState } from "react";

interface AddSniModalProps {
  open: boolean;
  domain: string;
  variants: string[];
  selected: string;
  sets: B4SetConfig[];
  createNewSet?: boolean;
  onClose: () => void;
  onSelectVariant: (variant: string) => void;
  onAdd: (setId: string, setName?: string) => void;
}

export const AddSniModal = ({
  open,
  domain,
  variants,
  selected,
  sets,
  createNewSet = false,
  onClose,
  onSelectVariant,
  onAdd,
}: AddSniModalProps) => {
  const [selectedSetId, setSelectedSetId] = useState<string>("");
  const [setName, setSetName] = useState<string>("");

  const handleAdd = () => {
    onAdd(selectedSetId, setName);
  };

  useEffect(() => {
    if (open) {
      if (createNewSet) {
        setSelectedSetId(NEW_SET_ID);
      } else if (sets.length > 0) {
        setSelectedSetId(MAIN_SET_ID);
      }
    }
  }, [open, sets, createNewSet]);

  return (
    <Dialog open={open} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <div className="flex items-center gap-3">
            <Button variant="secondary" size="icon" className="shrink-0">
              <DomainIcon />
            </Button>
            <DialogTitle>Add Domain to Manual List</DialogTitle>
          </div>
        </DialogHeader>
        <div className="space-y-4">
          <Alert>
            <AlertDescription>
              Select which domain pattern to add to the manual domains list.
              More specific patterns will only match exact subdomains, while
              broader patterns will match all subdomains.
            </AlertDescription>
          </Alert>
          <p className="text-muted-foreground text-sm">
            Original domain: <Badge>{domain}</Badge>
          </p>
          {!createNewSet && sets.length > 0 && (
            <SetSelector
              sets={sets}
              value={selectedSetId}
              onChange={(setId, name) => {
                setSelectedSetId(setId);
                if (name) setSetName(name);
              }}
            />
          )}
          <RadioGroup
            value={selected}
            onValueChange={(value) => onSelectVariant(value)}
          >
            {variants.map((variant, index) => {
              let description: string;
              if (index === 0) {
                description = "Most specific - exact match only";
              } else if (index === variants.length - 1) {
                description = "Broadest - matches all subdomains";
              } else {
                description = "Intermediate specificity";
              }
              return (
                <Label key={variant} htmlFor={`variant-${variant}`}>
                  <Field
                    orientation="horizontal"
                    className="has-[>[data-state=checked]]:bg-primary/5 dark:has-[>[data-state=checked]]:bg-primary/10 has-[>[data-checked]]:bg-primary/5 dark:has-[>[data-checked]]:bg-primary/10 border-border border p-2"
                  >
                    <FieldContent>
                      <FieldTitle>
                        <div className="font-medium">{variant}</div>
                      </FieldTitle>
                      <FieldDescription>{description}</FieldDescription>
                    </FieldContent>
                    <RadioGroupItem value={variant} id={`variant-${variant}`} />
                  </Field>
                </Label>
              );
            })}
          </RadioGroup>
        </div>
        <Separator />
        <DialogFooter>
          <Button onClick={onClose} variant="ghost">
            Cancel
          </Button>
          <div className="flex-1" />
          <Button onClick={handleAdd} disabled={!selected || !selectedSetId}>
            <AddIcon className="mr-2 size-4" />
            Add Domain
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
