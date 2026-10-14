import "@material/web/select/outlined-select.js";
import "@material/web/select/select-option.js";
import { Children, Fragment, isValidElement, useEffect, useRef, type ReactNode } from "react";

declare module "react" {
  namespace JSX {
    interface IntrinsicElements {
      "md-outlined-select": React.DetailedHTMLProps<React.HTMLAttributes<HTMLElement>, HTMLElement> & {
        value?: string;
        disabled?: boolean;
        label?: string;
        required?: boolean;
        "menu-positioning"?: string;
      };
      "md-select-option": React.DetailedHTMLProps<React.HTMLAttributes<HTMLElement>, HTMLElement> & {
        value?: string;
        selected?: boolean;
      };
    }
  }
}

type Choice = { value: string; label: string };

// choicesOf reads the <option> children a native <select> would take, so a
// screen does not care which implementation is behind Select.
export function choicesOf(children: ReactNode): Choice[] {
  return Children.toArray(children).flatMap((c): Choice[] => {
    if (!isValidElement<{ value?: string | number; children?: ReactNode }>(c)) return [];
    if (c.type === Fragment) return choicesOf(c.props.children);
    const label = Children.toArray(c.props.children).join("");
    return [{ value: String(c.props.value ?? label), label }];
  });
}

type Props = {
  value: string | number;
  onChange: (value: string) => void;
  disabled?: boolean;
  label?: string;
  required?: boolean;
  "aria-label"?: string;
  children: ReactNode;
};

export function Select({ value, onChange, children, ...rest }: Props) {
  const ref = useRef<HTMLElement & { value: string }>(null);
  const current = String(value);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const changed = () => onChange(el.value);
    el.addEventListener("change", changed);
    return () => el.removeEventListener("change", changed);
  }, [onChange]);

  return (
    // The popover menu opens above the page's scroll containers, which would clip it.
    <md-outlined-select ref={ref} value={current} menu-positioning="popover" {...rest}>
      {choicesOf(children).map((c) => (
        <md-select-option key={c.value} value={c.value} selected={c.value === current}>
          <div slot="headline">{c.label}</div>
        </md-select-option>
      ))}
    </md-outlined-select>
  );
}
