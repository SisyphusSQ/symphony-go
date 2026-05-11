import type { Meta, StoryObj } from "@storybook/vue3-vite";

import type { OperatorApiClient } from "../api/operator";
import Dashboard from "../pages/Dashboard.vue";
import { mockOperatorApiClient, mockRunDetails, mockState } from "../fixtures/operator";

const emptyClient: OperatorApiClient = {
  async getState() {
    return {
      data: {
        ...mockState,
        counts: {},
        running: [],
        retrying: [],
        latest_completed_or_failed: [],
        tokens: {
          input_tokens: 0,
          output_tokens: 0,
          reasoning_tokens: 0,
          cached_tokens: 0,
          total_tokens: 0,
        },
        runtime: { total_seconds: 0 },
      },
      source: "mock",
    };
  },
  async getRuns(filters) {
    return {
      data: {
        rows: [],
        limit: filters.limit,
      },
      source: "mock",
    };
  },
  async getRunDetail() {
    return {
      data: mockRunDetails["run-too-142-active"],
      source: "mock",
    };
  },
  async getRunEvents() {
    return {
      data: {
        rows: [],
        limit: 100,
      },
      source: "mock",
    };
  },
};

const errorClient: OperatorApiClient = {
  async getState() {
    return { data: mockState, source: "api" };
  },
  async getRuns() {
    throw new Error("operator API returned HTTP 503");
  },
  async getRunDetail() {
    return { data: mockRunDetails["run-too-142-active"], source: "api" };
  },
  async getRunEvents() {
    return {
      data: {
        rows: [],
        limit: 100,
      },
      source: "api",
    };
  },
};

const meta = {
  title: "Operator/Dashboard",
  component: Dashboard,
  tags: ["autodocs"],
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof Dashboard>;

export default meta;
type Story = StoryObj<typeof meta>;

export const MockData: Story = {
  name: "Mock Data",
  args: {
    client: mockOperatorApiClient,
  },
};

export const EmptyQueue: Story = {
  name: "Empty Queue",
  args: {
    client: emptyClient,
  },
};

export const ApiError: Story = {
  name: "API Error",
  args: {
    client: errorClient,
  },
};
