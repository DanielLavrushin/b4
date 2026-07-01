import { useEffect, useRef, useState } from "react";
import { B4TextField, type B4TextFieldProps } from "./B4TextField";

interface B4NumberFieldProps
  extends Omit<B4TextFieldProps, "value" | "onChange" | "type"> {
  value: number | null | undefined;
  onChange: (n: number) => void;
  min?: number;
  max?: number;
  allowDecimal?: boolean;
  helperText?: React.ReactNode;
}

export const B4NumberField = ({
  value,
  onChange,
  min,
  max,
  allowDecimal = false,
  onFocus,
  onBlur,
  ...props
}: B4NumberFieldProps) => {
  const [text, setText] = useState<string>(value == null ? "" : String(value));
  const focusedRef = useRef(false);

  useEffect(() => {
    if (!focusedRef.current) setText(value == null ? "" : String(value));
  }, [value]);

  const pattern = allowDecimal ? /^-?\d*\.?\d*$/ : /^-?\d*$/;

  return (
    <B4TextField
      {...props}
      value={text}
      inputMode={allowDecimal ? "decimal" : "numeric"}
      onFocus={(e) => {
        focusedRef.current = true;
        e.target.select();
        onFocus?.(e);
      }}
      onChange={(e) => {
        const v = e.target.value;
        if (!pattern.test(v)) return;
        setText(v);
        if (v === "" || v === "-" || v === ".") return;
        const n = Number(v);
        if (Number.isNaN(n)) return;
        if (min != null && n < min) return;
        if (max != null && n > max) return;
        if (n !== value) onChange(n);
      }}
      onBlur={(e) => {
        focusedRef.current = false;
        const empty = text === "" || text === "-" || text === ".";
        let n = empty ? Number(value ?? 0) : Number(text);
        if (Number.isNaN(n)) n = Number(value ?? 0);
        if (min != null) n = Math.max(min, n);
        if (max != null) n = Math.min(max, n);
        setText(String(n));
        if (n !== value) onChange(n);
        onBlur?.(e);
      }}
    />
  );
};

export default B4NumberField;
