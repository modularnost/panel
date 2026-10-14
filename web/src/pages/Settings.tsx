import { useState } from "react";
import { atLeast, useMe } from "../auth";
import { Button } from "../components/Button";
import { DEFAULT_SEED, setSeed, storedSeed } from "../theme";
import { Updates } from "./Updates";

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
      <h2>Appearance</h2>
      <div className="toolbar">
        <label className="swatch">
          <input type="color" value={seed} onChange={(e) => choose(e.target.value)} />
          Accent color
        </label>
        <Button variant="text" disabled={seed.toLowerCase() === DEFAULT_SEED} onClick={() => choose(DEFAULT_SEED)}>
          Reset
        </Button>
      </div>
      <p className="muted">Kept in this browser.</p>
      {atLeast(me.data?.role, "admin") && <Updates />}
    </>
  );
}
