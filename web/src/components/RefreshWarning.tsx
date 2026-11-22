import { retryableQueryError } from "../api/client";
import { Banner } from "./Banner";

export function RefreshWarning({ error }: { error: unknown }) {
  return retryableQueryError(error)
    ? <Banner tone="warning">Could not refresh. Showing the last loaded data.</Banner>
    : null;
}
