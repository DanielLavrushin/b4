import { TcpIcon, UdpIcon } from "@b4.icons";
import { Badge } from "@primitives/badge";
import { cn } from "@design/lib/utils";

interface ProtocolChipProps {
  protocol: "TCP" | "UDP";
}

export const ProtocolChip = ({ protocol }: ProtocolChipProps) => {
  const icon =
    protocol === "TCP" ? (
      <TcpIcon className="size-3" />
    ) : (
      <UdpIcon className="size-3" />
    );

  return (
    <Badge
      variant="outline"
      className={cn(
        protocol === "UDP" && "text-primary bg-primary/5 dark:bg-primary/10",
      )}
    >
      {icon}
      {protocol}
    </Badge>
  );
};
