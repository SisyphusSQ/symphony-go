import type { Meta, StoryObj } from "@storybook/vue3-vite";

import RunList from "../components/RunList.vue";
import { mockRuns } from "../fixtures/operator";

const meta = {
  title: "Operator/RunList",
  component: RunList,
  tags: ["autodocs"],
  args: {
    runs: mockRuns,
    loading: false,
    error: "",
    selectedRunID: "run-too-142-active",
  },
} satisfies Meta<typeof RunList>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Queue: Story = {};

export const Empty: Story = {
  args: {
    runs: [],
  },
};

export const ErrorState: Story = {
  args: {
    runs: [],
    error: "operator API request failed",
  },
};
