import { NewReleaseIcon } from "@b4.icons";
import { Badge } from "@design/components/ui/badge";
import { Spinner } from "@design/components/ui/spinner";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@design/components/ui/tooltip";
import { cn } from "@design/lib/utils";

interface VersionBadgeProps {
  version: string;
  hasUpdate?: boolean;
  isLoading?: boolean;
  onClick?: () => void;
}

export const VersionBadge = ({
  version,
  hasUpdate = false,
  isLoading = false,
  onClick,
}: VersionBadgeProps) => {
  if (isLoading) {
    return (
      <div className="flex items-center justify-center gap-2 p-2 h-8">
        <Spinner className="h-4 w-4" />
        <span className="text-muted-foreground text-xs">
          Checking for updates...
        </span>
      </div>
    );
  }

  return (
    <div
      className="flex items-center justify-center gap-2 p-2 cursor-pointer h-8"
      onClick={onClick}
    >
      {hasUpdate ? (
        <Tooltip>
          <TooltipTrigger asChild>
            <div>
              <Badge
                variant="secondary"
                className={cn(
                  "text-xs px-1.5 py-0.5 font-semibold animate-pulse hover:scale-105 transition-all inline-flex items-center gap-1"
                )}
              >
                <NewReleaseIcon className="h-3 w-3" />
                {`v${version}`}
              </Badge>
            </div>
          </TooltipTrigger>
          <TooltipContent side="right">
            <p>New version available! Click to view details</p>
          </TooltipContent>
        </Tooltip>
      ) : (
        <Badge variant="ghost">v{version}</Badge>
      )}
    </div>
  );
};
