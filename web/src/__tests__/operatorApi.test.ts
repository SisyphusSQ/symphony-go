import { describe, expect, it, vi } from "vitest";

import {
  buildRunsPath,
  createFetchOperatorApiClient,
  OperatorApiError,
  type OperatorApiClient,
} from "../api/operator";
import { mockOperatorApiClient, mockState } from "../fixtures/operator";

describe("operator API client", () => {
  it("builds stable run-list query parameters", () => {
    expect(
      buildRunsPath({
        statuses: ["running", "failed"],
        issue: "TOO-142",
        limit: 25,
      }),
    ).toBe("/api/v1/runs?limit=25&status=running%2Cfailed&issue=TOO-142");
  });

  it("decodes API error envelopes", async () => {
    const fetcher = vi.fn(async () =>
      new Response(JSON.stringify({ error: { code: "run_not_found", message: "Run not found" } }), {
        status: 404,
      }),
    );
    const client = createFetchOperatorApiClient({
      baseURL: "http://127.0.0.1:4002",
      fallbackMode: "never",
      fetcher,
    });

    await expect(client.getRunDetail("missing")).rejects.toMatchObject({
      code: "run_not_found",
      status: 404,
    });
  });

  it("falls back to mock data when live API is unavailable", async () => {
    const fetcher = vi.fn(async () => {
      throw new TypeError("connect ECONNREFUSED");
    });
    const client = createFetchOperatorApiClient({
      fallback: mockOperatorApiClient,
      fetcher,
    });

    const result = await client.getState();

    expect(result.source).toBe("mock");
    expect(result.fallbackReason).toContain("connect ECONNREFUSED");
    expect(result.data.lifecycle.state).toBe(mockState.lifecycle.state);
  });

  it("throws when mock-only mode has no fallback", async () => {
    const client = createFetchOperatorApiClient({
      fallbackMode: "always",
    }) as OperatorApiClient;

    await expect(client.getState()).rejects.toBeInstanceOf(OperatorApiError);
  });
});
