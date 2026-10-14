import { DEFAULT_SEED } from "../theme";

export const PRESETS = [
  { name: "Navy", hex: DEFAULT_SEED },
  { name: "Blue", hex: "#0061a4" },
  { name: "Teal", hex: "#006a60" },
  { name: "Green", hex: "#386a20" },
  { name: "Amber", hex: "#8d5000" },
  { name: "Red", hex: "#b3261e" },
  { name: "Rose", hex: "#984061" },
  { name: "Purple", hex: "#6750a4" },
];

const Check = () => (
  <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
    <path d="M9 16.2 4.8 12l-1.4 1.4L9 19 21 7l-1.4-1.4z" fill="currentColor" />
  </svg>
);

type Props = { value: string; onChange: (hex: string) => void };

// Presets are radios; the last swatch opens the browser's own picker for any other color.
export function Swatches({ value, onChange }: Props) {
  const current = value.toLowerCase();
  const custom = !PRESETS.some((p) => p.hex === current);

  return (
    <div className="swatches" role="radiogroup" aria-label="Accent color">
      {PRESETS.map((p) => (
        <label key={p.hex} className={`swatch-option${p.hex === current ? " selected" : ""}`} style={{ background: p.hex }} title={p.name}>
          <input type="radio" name="accent" aria-label={p.name} checked={p.hex === current} onChange={() => onChange(p.hex)} />
          <Check />
        </label>
      ))}
      <label
        className={`swatch-option custom${custom ? " selected" : ""}`}
        style={custom ? { background: current } : undefined}
        title="Custom"
      >
        <input type="color" aria-label="Custom color" value={current} onChange={(e) => onChange(e.target.value)} />
        <Check />
      </label>
    </div>
  );
}
