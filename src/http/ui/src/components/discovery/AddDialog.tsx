import { AddIcon } from "@b4.icons";
import { Badge } from "@design/components/ui/badge";
import { Button } from "@design/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@design/components/ui/dialog";
import { Field, FieldLabel } from "@design/components/ui/field";
import { Input } from "@design/components/ui/input";
import { Label } from "@design/components/ui/label";
import { RadioGroup, RadioGroupItem } from "@design/components/ui/radio-group";
import { Separator } from "@design/components/ui/separator";
import { Spinner } from "@design/components/ui/spinner";
import { cn } from "@design/lib/utils";
import { B4SetConfig } from "@models/config";
import { generateDomainVariants } from "@utils";
import { useEffect, useState } from "react";

interface SimilarSet {
  id: string;
  name: string;
  domains: string[];
}

interface DiscoveryAddDialogProps {
  open: boolean;
  domain: string;
  presetName: string;
  setConfig: B4SetConfig | null;
  onClose: () => void;
  onAddNew: (name: string, domain: string) => void;
  onAddToExisting: (setId: string, domain: string) => void;
  loading?: boolean;
}

export const DiscoveryAddDialog = ({
  open,
  domain,
  presetName,
  setConfig,
  onClose,
  onAddNew,
  onAddToExisting,
  loading = false,
}: DiscoveryAddDialogProps) => {
  const [name, setName] = useState(presetName);
  const [variants, setVariants] = useState<string[]>([]);
  const [selectedVariant, setSelectedVariant] = useState(domain);
  const [mode, setMode] = useState<"new" | "existing">("new");
  const [similarSets, setSimilarSets] = useState<SimilarSet[]>([]);
  const [selectedSetId, setSelectedSetId] = useState<string | null>(null);

  useEffect(() => {
    if (open && domain) {
      const v = generateDomainVariants(domain);
      setVariants(v);
      setSelectedVariant(v[0] || domain);
      setName(presetName);
      setMode("new");
      setSelectedSetId(null);
    }
  }, [open, domain, presetName]);

  // Fetch similar sets when dialog opens
  useEffect(() => {
    if (!open || !setConfig) return;

    const fetchSimilar = async () => {
      try {
        const response = await fetch("/api/discovery/similar", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(setConfig),
        });
        if (response.ok) {
          const data = (await response.json()) as SimilarSet[];
          setSimilarSets(data);
          if (data.length > 0) {
            setSelectedSetId(data[0].id);
          }
        }
      } catch {
        setSimilarSets([]);
      }
    };

    void fetchSimilar();
  }, [open, setConfig]);

  const handleConfirm = () => {
    if (mode === "new") {
      onAddNew(name, selectedVariant);
    } else if (selectedSetId) {
      onAddToExisting(selectedSetId, selectedVariant);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <div className="flex items-center gap-3">
            <div className="flex h-10 w-10 items-center justify-center rounded-md bg-accent text-accent-foreground">
              <AddIcon />
            </div>
            <div className="flex-1">
              <DialogTitle>Add Configuration</DialogTitle>
              <DialogDescription className="mt-1">
                Strategy: {presetName}
              </DialogDescription>
            </div>
          </div>
        </DialogHeader>
        <div className="py-4">
          <div className="space-y-6 mt-2">
            {/* Domain variant selection */}
            <div>
              <h6 className="text-sm font-semibold mb-2 text-muted-foreground">
                Domain Pattern
              </h6>
              <div className="flex flex-row gap-2 flex-wrap">
                {variants.map((v) => (
                  <Badge
                    key={v}
                    onClick={() => setSelectedVariant(v)}
                    className={cn(
                      "cursor-pointer transition-all",
                      v === selectedVariant
                        ? "bg-accent border-2 border-secondary"
                        : "bg-muted border border-border"
                    )}
                  >
                    {v}
                  </Badge>
                ))}
              </div>
            </div>

            {/* Mode selection - only show if similar sets exist */}
            {similarSets.length > 0 && (
              <div>
                <h6 className="text-sm font-semibold mb-2 text-muted-foreground">
                  Add to
                </h6>
                <RadioGroup
                  value={mode}
                  onValueChange={(value) =>
                    setMode(value as "new" | "existing")
                  }
                >
                  <div className="flex items-center space-x-2 mb-2">
                    <RadioGroupItem value="new" id="mode-new" />
                    <Label htmlFor="mode-new" className="cursor-pointer">
                      Create new set
                    </Label>
                  </div>
                  <div className="flex items-center space-x-2">
                    <RadioGroupItem value="existing" id="mode-existing" />
                    <Label htmlFor="mode-existing" className="cursor-pointer">
                      Add to existing similar set
                    </Label>
                  </div>
                </RadioGroup>
              </div>
            )}

            {/* New set name input */}
            {mode === "new" && (
              <Field>
                <FieldLabel>Set Name</FieldLabel>
                <Input
                  value={name}
                  onChange={(e: React.ChangeEvent<HTMLInputElement>) =>
                    setName(e.target.value)
                  }
                />
              </Field>
            )}

            {/* Similar sets list */}
            {mode === "existing" && similarSets.length > 0 && (
              <div>
                <h6 className="text-sm font-semibold mb-2 text-muted-foreground">
                  Similar Sets
                </h6>
                <div className="space-y-2">
                  {similarSets.map((set) => (
                    <div
                      key={set.id}
                      onClick={() => setSelectedSetId(set.id)}
                      className={cn(
                        "p-4 rounded-md cursor-pointer transition-all",
                        set.id === selectedSetId
                          ? "bg-accent border-2 border-secondary"
                          : "bg-muted border border-border"
                      )}
                    >
                      <p className="font-semibold">{set.name}</p>
                      <p className="text-xs text-muted-foreground">
                        {set.domains.slice(0, 3).join(", ")}
                        {set.domains.length > 3 &&
                          ` +${set.domains.length - 3} more`}
                      </p>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
        </div>
        <Separator />
        <DialogFooter>
          <div className="flex gap-4">
            <Button onClick={onClose} variant="ghost" disabled={loading}>
              Cancel
            </Button>
            <Button
              onClick={handleConfirm}
              disabled={loading || (mode === "existing" && !selectedSetId)}
            >
              {loading ? (
                <>
                  <Spinner className="h-4 w-4 mr-2" />
                  Adding...
                </>
              ) : (
                <>
                  <AddIcon className="h-4 w-4 mr-2" />
                  {mode === "new" ? "Create Set" : "Add to Set"}
                </>
              )}
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
