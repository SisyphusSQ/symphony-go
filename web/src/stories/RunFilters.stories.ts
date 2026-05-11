import type { Meta, StoryObj } from "@storybook/vue3-vite";

import { defaultRunFilters } from "../api/operator";
import RunFilters from "../components/RunFilters.vue";

const meta = {
  title: "Operator/RunFilters",
  component: RunFilters,
  tags: ["autodocs"],
  args: {
    filters: defaultRunFilters,
    loading: false,
  },
} satisfies Meta<typeof RunFilters>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {};

export const Loading: Story = {
  args: {
    loading: true,
  },
};

export const FailedRuns: Story = {
  args: {
    filters: {
      statuses: ["failed"],
      issue: "TOO-140",
      limit: 50,
    },
  },
};
