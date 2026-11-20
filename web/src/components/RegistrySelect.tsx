import { useQuery } from "@tanstack/react-query";
import { api, call } from "../api/client";
import { Select } from "./Select";

export function RegistrySelect({ value, onChange }: { value: string; onChange: (value: string) => void }) {
  const saved = useQuery({ queryKey: ["registries"], queryFn: () => call(api.listRegistries()) });
  const registries = saved.data?.registries ?? [];
  if (!value && (!saved.data?.configured || registries.length === 0)) return null;

  return (
    <Select label="Private registry" value={value} onChange={onChange}>
      <option value="">None</option>
      {value && !registries.some(({ host }) => host === value) && <option value={value}>{value}</option>}
      {registries.map(({ host }) => <option key={host} value={host}>{host}</option>)}
    </Select>
  );
}
