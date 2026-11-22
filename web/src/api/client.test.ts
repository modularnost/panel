import { QueryClient, QueryObserver } from "@tanstack/react-query";
import { expect, test } from "vitest";
import { ApiError, call, retryableQueryError } from "./client";

test("a transient failed refresh retains the loaded rows until the next successful fetch", async () => {
  const client = new QueryClient();
  let status: number | undefined;
  const observer = new QueryObserver(client, {
    queryKey: ["nodes"],
    retry: false,
    queryFn: async () => {
      if (status !== undefined) throw new ApiError(status, "refresh failed");
      return ["manager"];
    },
  });
  expect((await observer.refetch()).data).toEqual(["manager"]);
  status = 503;
  const failed = await observer.refetch();
  expect(failed.isRefetchError).toBe(true);
  expect(failed.data).toEqual(["manager"]);
  expect(retryableQueryError(failed.error)).toBe(true);
  status = 401;
  expect(retryableQueryError((await observer.refetch()).error)).toBe(false);
  status = undefined;
  expect((await observer.refetch()).error).toBeNull();
  client.clear();
});

test("fetch failures and malformed proxy responses produce readable errors", async () => {
  await expect(call(Promise.reject(new TypeError("failed to fetch")))).rejects.toMatchObject({
    status: 0, message: "Could not reach the panel.",
  });
  await expect(call(Promise.reject({ status: 502, error: "<html>Bad Gateway</html>" }))).rejects.toMatchObject({
    status: 502, message: "Request failed (HTTP 502).",
  });
});
