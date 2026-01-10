import { colors } from "@design";
import DecryptedText from "@common/DecryptedText";
import { cn } from "@design/lib/utils";

interface LogoProps {
  className?: string;
}

export function Logo({ className }: LogoProps) {
  return (
    <div className={cn("flex flex-col gap-0", className)}>
      <div
        className="text-[0.65rem] opacity-70 tracking-widest uppercase"
        style={{
          color: colors.text.secondary,
        }}
      >
        <DecryptedText text="Bye Bye Big Bro" />
      </div>
    </div>
  );
}
