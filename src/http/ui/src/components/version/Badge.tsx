import { NewReleaseIcon } from "@b4.icons";
import { Badge } from "@primitives/badge";
import { Button } from "@primitives/button";
import { Spinner } from "@primitives/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@primitives/tooltip";

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
          <TooltipTrigger asChild>
            <Badge onClick={onClick}>
              <NewReleaseIcon />
              {`v${version}`}
            </Badge>
          </TooltipTrigger>
          <TooltipContent side="right">
            <p>New version available! Click to view details</p>
          </TooltipContent>
        </Tooltip>
      ) : (
        <Badge variant="ghost" onClick={onClick}>
          v{version}
        </Badge>
      )}
    </>
  );
};
