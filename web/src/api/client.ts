import { Api, type Problem } from "./generated";

// The API refuses a write that is not application/json, even one without a body.
const client = new Api({
  baseUrl: "",
  baseApiParams: { headers: { "Content-Type": "application/json" } },
});

export const api = client.api;

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

export function retryableQueryError(error: unknown): boolean {
  return error instanceof ApiError &&
    (error.status === 0 || error.status === 408 || error.status === 429 || error.status >= 500);
}

// call turns what the generated client throws (the failed response, with the
// parsed {error: {code, message}} in .error) into an ApiError.
export async function call<T>(request: Promise<T>): Promise<T> {
  try {
    return await request;
  } catch (e) {
    const failed = e as { status?: number; error?: Problem | null; clone?: () => Response };
    const status = failed.status ?? 0;
    let message = failed.error?.error?.message;
    if (!message && typeof failed.clone === "function") {
      try {
        const problem = (await failed.clone().json()) as Problem;
        message = problem?.error?.message;
      } catch {
        // Non-JSON error bodies fall back to the HTTP status message below.
      }
    }
    throw new ApiError(status, message ??
      (status === 0 ? "Could not reach the panel." : `Request failed (HTTP ${status}).`));
  }
}
