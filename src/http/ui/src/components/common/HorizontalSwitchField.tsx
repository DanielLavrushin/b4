import {
  Field,
  FieldContent,
  FieldDescription,
  FieldTitle,
} from "@design/components/ui/field";
import { Switch } from "@design/components/ui/switch";
import { cn } from "@design/lib/utils";

interface HorizontalSwitchFieldProps {
  id: string;
  title: string | React.ReactNode;
  description?: string | React.ReactNode;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
  className?: string;
  disabled?: boolean;
}

export const HorizontalSwitchField = ({
  id,
  title,
  description,
  checked,
  onCheckedChange,
  className,
  disabled,
}: HorizontalSwitchFieldProps) => {
  return (
    <label htmlFor={id}>
      <Field
        orientation="horizontal"
        className={cn(
          "has-[>[data-state=checked]]:bg-primary/5 dark:has-[>[data-state=checked]]:bg-primary/10 has-[>[data-checked]]:bg-primary/5 dark:has-[>[data-checked]]:bg-primary/10 p-2",
          className
        )}
      >
        <FieldContent>
          <FieldTitle>{title}</FieldTitle>
          {description && <FieldDescription>{description}</FieldDescription>}
        </FieldContent>
        <Switch
          id={id}
          checked={checked}
          onCheckedChange={onCheckedChange}
          disabled={disabled}
        />
      </Field>
    </label>
  );
};

