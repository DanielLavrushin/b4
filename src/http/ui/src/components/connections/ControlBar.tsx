import { ClearIcon } from "@b4.icons";
import { Badge } from "@design/components/ui/badge";
import { Button } from "@design/components/ui/button";
import { Input } from "@design/components/ui/input";
import { Label } from "@design/components/ui/label";
import { Switch } from "@design/components/ui/switch";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@design/components/ui/tooltip";

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
  onClearSort: () => void;
  onReset: () => void;
}

export const DomainsControlBar = ({
  filter,
  onFilterChange,
  totalCount,
  filteredCount,
  sortColumn,
  paused,
  showAll,
  onShowAllChange,
  onPauseChange,
  onClearSort,
  onReset,
}: DomainsControlBarProps) => {
  return (
    <div className="p-4 border-b border-border bg-muted/50">
      <div className="flex flex-row gap-4 items-center">
        <Input
          placeholder="Filter (combine with +, exclude with !, e.g. tcp+!domain:google.com)"
          value={filter}
          onChange={(e) => onFilterChange(e.target.value)}
          className="flex-1"
        />
        <div className="flex flex-row gap-2 items-center">
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
          <Label className="font-medium cursor-pointer">
            {showAll ? "All packets" : "Domains only"}
          </Label>
        </div>
        <div className="flex items-center gap-2">
          <Switch
            checked={paused}
            onCheckedChange={(checked: boolean) => onPauseChange(checked)}
          />
          <Label className="font-medium cursor-pointer">
            {paused ? "Paused" : "Streaming"}
          </Label>
        </div>
        <Tooltip>
          <TooltipTrigger asChild>
            <Button variant="ghost" size="icon-sm" onClick={onReset}>
              <ClearIcon />
            </Button>
          </TooltipTrigger>
          <TooltipContent>
            <p>Clear Connections</p>
          </TooltipContent>
        </Tooltip>
      </div>
    </div>
  );
};
