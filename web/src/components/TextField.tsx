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
  const ref = useRef<HTMLElement & { value: string; form: HTMLFormElement | null; updateComplete: Promise<unknown> }>(null);
  const multiline = rest.type === "textarea";

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const input = () => onChange(el.value);
    // Material Web does not submit its form on Enter the way an <input> does.
    const enter = (e: KeyboardEvent) => {
      if (e.key === "Enter" && !multiline && !e.isComposing) el.form?.requestSubmit();
    };
    el.addEventListener("input", input);
    el.addEventListener("keydown", enter);
    return () => {
      el.removeEventListener("input", input);
      el.removeEventListener("keydown", enter);
    };
  }, [onChange, multiline]);

  useEffect(() => {
    if (autoFocus) ref.current?.focus();
  }, [autoFocus]);

  // The field has no spellcheck option and the attribute does not cross its shadow root.
  const { spellCheck } = rest;
  useEffect(() => {
    const el = ref.current;
    if (spellCheck !== false || !el) return;
    void el.updateComplete.then(() => el.shadowRoot?.querySelector("input, textarea")?.setAttribute("spellcheck", "false"));
  }, [spellCheck]);

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
