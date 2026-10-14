import "@material/web/switch/switch.js";
import { useEffect, useRef } from "react";

declare module "react" {
  namespace JSX {
    interface IntrinsicElements {
      "md-switch": React.DetailedHTMLProps<React.HTMLAttributes<HTMLElement>, HTMLElement> & {
        selected?: boolean;
        disabled?: boolean;
      };
    }
  }
}

type Props = {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
};

export function Switch({ label, checked, onChange, disabled }: Props) {
  const ref = useRef<HTMLElement & { selected: boolean }>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const changed = () => onChange(el.selected);
    el.addEventListener("change", changed);
    return () => el.removeEventListener("change", changed);
  }, [onChange]);

  return (
    <label className="switch">
      <md-switch ref={ref} selected={checked} disabled={disabled} aria-label={label} />
      {label}
    </label>
  );
}
