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

// call turns what the generated client throws (the failed response, with the
// parsed {error: {code, message}} in .error) into an ApiError.
export async function call<T>(request: Promise<T>): Promise<T> {
  try {
    return await request;
  } catch (e) {
    const failed = e as { status?: number; error?: Problem | null };
    throw new ApiError(failed.status ?? 0, failed.error?.error.message ?? "request failed");
  }
}
