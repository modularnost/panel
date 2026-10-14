import "@material/web/textfield/outlined-text-field.js";
import { useEffect, useRef } from "react";

declare module "react" {
  namespace JSX {
    interface IntrinsicElements {
      "md-outlined-text-field": React.DetailedHTMLProps<React.HTMLAttributes<HTMLElement>, HTMLElement> & {
        label?: string;
        value?: string;
        type?: string;
        required?: boolean;
        minLength?: number;
        pattern?: string;
        placeholder?: string;
        autocomplete?: string;
        rows?: number;
        error?: boolean;
        "error-text"?: string;
        disabled?: boolean;
      };
    }
  }
}

type Props = {
  label: string;
  value: string;
  onChange: (value: string) => void;
  type?: "text" | "password" | "textarea";
  rows?: number;
  required?: boolean;
  minLength?: number;
  pattern?: string;
  placeholder?: string;
  inputMode?: "numeric";
  autoComplete?: string;
  autoFocus?: boolean;
  spellCheck?: boolean;
  disabled?: boolean;
  // A message turns the field red and is shown under it.
  error?: string;
  mono?: boolean;
};

export function TextField({ onChange, value, autoFocus, error, mono, autoComplete, ...rest }: Props) {
  const ref = useRef<HTMLElement & { value: string }>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const input = () => onChange(el.value);
    el.addEventListener("input", input);
    return () => el.removeEventListener("input", input);
  }, [onChange]);

  useEffect(() => {
    if (autoFocus) ref.current?.focus();
  }, [autoFocus]);

  return (
    <md-outlined-text-field
      ref={ref}
      value={value}
      autocomplete={autoComplete}
      error={!!error}
      error-text={error}
      className={mono ? "mono" : undefined}
      {...rest}
    />
  );
}
