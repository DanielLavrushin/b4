import { B4Icon } from "@b4.icons";
import DecryptedText from "@common/DecryptedText";
import { Button } from "@design/primitives/button";
import { Item } from "@design/primitives/item";
import { ItemMedia } from "@design/primitives/item";

interface LogoProps {
  className?: string;
}

export function Logo({ className }: LogoProps) {
  return (

        <Item className="p-2">
          <ItemMedia>
            <Button size="icon" className="size-10">
              <B4Icon className="size-6" />
            </Button>
            <DecryptedText text="Bye Bye Big Bro" animateOn="both"/>
          </ItemMedia>
        </Item>

  );
}
