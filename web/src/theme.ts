import {
  Hct,
  MaterialDynamicColors as M,
  SchemeTonalSpot,
  argbFromHex,
  hexFromArgb,
  type DynamicColor,
} from "@material/material-color-utilities";

export const DEFAULT_SEED = "#040b4d";
const SEED_KEY = "panel.seed";

// The roles of the M3 color system, as --md-sys-color-<name> custom properties:
// the names @material/web components read as well.
const ROLES: Record<string, DynamicColor> = {
  primary: M.primary,
  "on-primary": M.onPrimary,
  "primary-container": M.primaryContainer,
  "on-primary-container": M.onPrimaryContainer,
  secondary: M.secondary,
  "on-secondary": M.onSecondary,
  "secondary-container": M.secondaryContainer,
  "on-secondary-container": M.onSecondaryContainer,
  tertiary: M.tertiary,
  "on-tertiary": M.onTertiary,
  "tertiary-container": M.tertiaryContainer,
  "on-tertiary-container": M.onTertiaryContainer,
  error: M.error,
  "on-error": M.onError,
  "error-container": M.errorContainer,
  "on-error-container": M.onErrorContainer,
  background: M.background,
  "on-background": M.onBackground,
  surface: M.surface,
  "surface-dim": M.surfaceDim,
  "surface-bright": M.surfaceBright,
  "surface-container-lowest": M.surfaceContainerLowest,
  "surface-container-low": M.surfaceContainerLow,
  "surface-container": M.surfaceContainer,
  "surface-container-high": M.surfaceContainerHigh,
  "surface-container-highest": M.surfaceContainerHighest,
  "on-surface": M.onSurface,
  "surface-variant": M.surfaceVariant,
  "on-surface-variant": M.onSurfaceVariant,
  outline: M.outline,
  "outline-variant": M.outlineVariant,
  "inverse-surface": M.inverseSurface,
  "inverse-on-surface": M.inverseOnSurface,
  "inverse-primary": M.inversePrimary,
  "surface-tint": M.surfaceTint,
  shadow: M.shadow,
  scrim: M.scrim,
};

function declarations(seed: string, dark: boolean): string {
  const scheme = new SchemeTonalSpot(Hct.fromInt(argbFromHex(seed)), dark, 0);
  return Object.entries(ROLES)
    .map(([name, role]) => `--md-sys-color-${name}:${hexFromArgb(role.getArgb(scheme))};`)
    .join("");
}

// themeCss is the light scheme on :root and the dark one when the system asks for it.
export function themeCss(seed: string): string {
  return (
    `:root{${declarations(seed, false)}}` +
    `@media (prefers-color-scheme: dark){:root{${declarations(seed, true)}}}`
  );
}

const HEX = /^#[0-9a-f]{6}$/i;

export function storedSeed(): string {
  try {
    const s = localStorage.getItem(SEED_KEY);
    if (s && HEX.test(s)) return s;
  } catch {
    // storage can be blocked; the default seed is fine
  }
  return DEFAULT_SEED;
}

// applyTheme paints the whole interface from one seed color; calling it again
// with another seed repaints without a reload.
export function applyTheme(seed: string = storedSeed()): void {
  let el = document.getElementById("m3-theme");
  if (!el) {
    el = document.createElement("style");
    el.id = "m3-theme";
    document.head.append(el);
  }
  el.textContent = themeCss(HEX.test(seed) ? seed : DEFAULT_SEED);
}

export function setSeed(seed: string): void {
  if (!HEX.test(seed)) return;
  try {
    localStorage.setItem(SEED_KEY, seed);
  } catch {
    // applied for this page view only
  }
  applyTheme(seed);
}
