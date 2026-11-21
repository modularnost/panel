import { formatTime } from "../format";

export function ActivityTime({ value }: { value?: string | null }) {
  return value ? <time dateTime={value}>{formatTime(value)}</time> : <span className="muted">Unknown</span>;
}
