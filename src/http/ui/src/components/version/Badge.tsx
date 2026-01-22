import { NewReleaseIcon } from "@b4.icons";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@design/primitives/tooltip";
import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import { Spinner } from "@primitives/spinner";

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
      <Button variant="ghost" disabled>
        <Spinner />
        Checking for updates...
      </Button>
    );
  }

  return (
    <>
      {hasUpdate ? (
        <Tooltip>
          <TooltipTrigger className="w-fit self-center">
            <Button onClick={onClick}>
              <NewReleaseIcon />
              {`v${version}`}
            </Button>
          </TooltipTrigger>

          <TooltipContent side="right">
            <p>New version available! Click to view details</p>
          </TooltipContent>
        </Tooltip>
      ) : (
        <Button variant="ghost" onClick={onClick} className="w-fit self-center">
          v{version}
        </Button>
      )}
    </>
  );
};
