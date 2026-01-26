import { ClearIcon } from "@b4.icons";
import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import { Input } from "@primitives/input";
import { Label } from "@primitives/label";
import { Switch } from "@primitives/switch";

interface DomainsControlBarProps {
  filter: string;
  onFilterChange: (filter: string) => void;
  totalCount: number;
  filteredCount: number;
  sortColumn: string | null;
  paused: boolean;
  onPauseChange: (paused: boolean) => void;
  showAll: boolean;
  onShowAllChange: (showAll: boolean) => void;
  onReset: () => void;
}

export const DomainsControlBar = ({
  filter,
  onFilterChange,
  totalCount,
  filteredCount,
  paused,
  showAll,
  onShowAllChange,
  onPauseChange,
  onReset,
}: DomainsControlBarProps) => {
  return (
    <div className="border-border bg-muted/50 border-b p-4">
      <div className="flex flex-row items-center gap-4">
        <Input
          placeholder="Filter (combine with +, exclude with !, e.g. tcp+!domain:google.com)"
          value={filter}
          onChange={(e) => onFilterChange(e.target.value)}
          className="flex-1"
        />
        <div className="flex flex-row items-center gap-2">
          <Badge>{`${totalCount} connections`}</Badge>
          {filter && (
            <Badge variant="outline">{`${filteredCount} filtered`}</Badge>
          )}
        </div>
        <div className="flex items-center gap-2">
          <Switch
            checked={showAll}
            onCheckedChange={(checked: boolean) => onShowAllChange(checked)}
          />
          <Label className="cursor-pointer font-medium">
            {showAll ? "All packets" : "Domains only"}
          </Label>
        </div>
        <div className="flex items-center gap-2">
          <Switch
            checked={paused}
            onCheckedChange={(checked: boolean) => onPauseChange(checked)}
          />
          <Label className="cursor-pointer font-medium">
            {paused ? "Paused" : "Streaming"}
          </Label>
        </div>

        <Button variant="ghost" size="icon-sm" onClick={onReset}>
          <ClearIcon />
        </Button>
      </div>
    </div>
  );
};
