import { useState } from "react";
import { atLeast, useMe } from "../auth";
import { Swatches } from "../components/Swatches";
import { setSeed, setTheme, storedSeed, storedTheme, type ThemeMode } from "../theme";
import { Updates } from "./Updates";
import { Registries } from "./Registries";
import "./Settings.css";

const THEME_OPTIONS: { value: ThemeMode; label: string }[] = [
  { value: "system", label: "System" },
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
];

export function Settings() {
  const me = useMe();
  const [seed, setSeedState] = useState(storedSeed);
  const [theme, setThemeState] = useState(storedTheme);

  const choose = (hex: string) => {
    setSeed(hex);
    setSeedState(hex);
  };

  const chooseTheme = (mode: ThemeMode) => {
    setTheme(mode);
    setThemeState(mode);
  };

  return (
    <>
      <h1>Settings</h1>
      <div className="settings">
        <section className="settings-card" aria-labelledby="settings-appearance">
          <h2 id="settings-appearance">Appearance</h2>
          <div className="settings-row">
            <p className="muted">Theme</p>
            <div className="segmented" role="group" aria-label="Theme">
              {THEME_OPTIONS.map((option) => (
                <button
                  key={option.value}
                  type="button"
                  className="segmented-btn"
                  aria-pressed={theme === option.value}
                  onClick={() => chooseTheme(option.value)}
                >
                  {option.label}
                </button>
              ))}
            </div>
          </div>
          <div className="settings-row">
            <p className="muted">Accent color</p>
            <Swatches value={seed} onChange={choose} />
          </div>
        </section>
        {atLeast(me.data?.role, "admin") && (
          <>
            <section className="settings-card" aria-labelledby="settings-updates">
              <h2 id="settings-updates">Updates</h2>
              <Updates />
            </section>
            <Registries />
          </>
        )}
      </div>
    </>
  );
}
