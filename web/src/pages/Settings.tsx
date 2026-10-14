import { useState } from "react";
import { atLeast, useMe } from "../auth";
import { Swatches } from "../components/Swatches";
import { setSeed, storedSeed } from "../theme";
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
      <Swatches value={seed} onChange={choose} />
      <p className="muted">Kept in this browser.</p>
      {atLeast(me.data?.role, "admin") && <Updates />}
    </>
  );
}
