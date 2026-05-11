import type { Meta, StoryObj } from "@storybook/vue3-vite";

import StatusBar from "../components/StatusBar.vue";
import { mockState } from "../fixtures/operator";

const meta = {
  title: "Operator/StatusBar",
  component: StatusBar,
  tags: ["autodocs"],
  args: {
    state: mockState,
    loading: false,
    source: "mock",
    fallbackReason: "",
  },
} satisfies Meta<typeof StatusBar>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Live: Story = {
  args: {
    source: "api",
  },
};

export const MockFallback: Story = {
  args: {
    fallbackReason: "connect ECONNREFUSED 127.0.0.1:4002",
  },
};

export const NotReady: Story = {
  args: {
    state: {
      ...mockState,
      lifecycle: { state: "degraded" },
      ready: {
        ok: false,
        error: "state store unavailable",
      },
      rate_limit: {
        latest: {
          primary_remaining: 240,
          reset_seconds: 390,
        },
        updated_at: mockState.rate_limit.updated_at,
      },
    },
  },
};
