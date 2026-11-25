import { useState } from "react";
import { atLeast, useMe } from "../auth";
import { Swatches } from "../components/Swatches";
import { setSeed, storedSeed } from "../theme";
import { Updates } from "./Updates";
import { Registries } from "./Registries";
import "./Settings.css";

export function Settings() {
  const me = useMe();
  const [seed, setSeedState] = useState(storedSeed);

  const choose = (hex: string) => {
    setSeed(hex);
    setSeedState(hex);
  };

  return (
    <>
      <h1>Settings</h1>
      <div className="settings">
        <section className="settings-card" aria-labelledby="settings-appearance">
          <div className="settings-row">
            <div>
              <h2 id="settings-appearance">Appearance</h2>
              <p className="muted">Accent color</p>
            </div>
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
