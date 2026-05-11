import { describe, expect, it, vi } from "vitest";

import {
  buildRunEventsPath,
  buildRunsPath,
  createFetchOperatorApiClient,
} from "../api/operator";
import { mockOperatorApiClient } from "../fixtures/operator";

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

  it("builds stable run-events query parameters", () => {
    expect(
      buildRunEventsPath("run/with spaces", {
        category: "tool",
        limit: 20,
      }),
    ).toBe("/api/v1/runs/run%2Fwith%20spaces/events?limit=20&category=tool");

    expect(
      buildRunEventsPath("run-too-142-active", {
        category: "all",
        limit: 100,
      }),
    ).toBe("/api/v1/runs/run-too-142-active/events?limit=100");
  });

  it("decodes API error envelopes", async () => {
    const fetcher = vi.fn(async () =>
      new Response(JSON.stringify({ error: { code: "run_not_found", message: "Run not found" } }), {
        status: 404,
      }),
    );
    const client = createFetchOperatorApiClient({
      baseURL: "http://127.0.0.1:4002",
      fetcher,
    });

    await expect(client.getRunDetail("missing")).rejects.toMatchObject({
      code: "run_not_found",
      status: 404,
    });
  });

  it("does not replace live API failures with mock data", async () => {
    const fetcher = vi.fn(async () => {
      throw new TypeError("connect ECONNREFUSED");
    });
    const client = createFetchOperatorApiClient({
      fetcher,
    });

    await expect(client.getState()).rejects.toThrow("connect ECONNREFUSED");
  });

  it("loads run events and filters mock timeline categories", async () => {
    const result = await mockOperatorApiClient.getRunEvents("run-too-142-active", {
      category: "tool",
      limit: 20,
    });

    expect(result.source).toBe("mock");
    expect(result.data.rows).toHaveLength(1);
    expect(result.data.rows[0].summary).toContain("linear_graphql");
  });
});
