import type { Meta, StoryObj } from "@storybook/vue3-vite";

import AdminDashboardView from "../components/admin/AdminDashboardView.vue";
import AdminRunDetailView from "../components/admin/AdminRunDetailView.vue";
import AdminRunsView from "../components/admin/AdminRunsView.vue";
import OperatorAdminShell from "../components/admin/OperatorAdminShell.vue";
import { defaultRunFilters } from "../api/operator";
import { mockRunDetails, mockRuns, mockState, mockTimelinePages } from "../fixtures/operator";

const selectedDetail = mockRunDetails["run-too-142-active"];
const selectedEvents = mockTimelinePages["run-too-142-active"].rows;

const meta = {
  title: "Operator/AdminShell",
  component: OperatorAdminShell,
  tags: ["autodocs"],
  parameters: {
    layout: "fullscreen",
  },
} satisfies Meta<typeof OperatorAdminShell>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Dashboard: Story = {
  args: {
    activeView: "dashboard",
    title: "Operator Overview",
    source: "mock",
    ready: true,
  },
  render: () => ({
    components: { AdminDashboardView, OperatorAdminShell },
    setup() {
      return { mockRuns, mockState, selectedDetail, selectedEvents };
    },
    template: `
      <OperatorAdminShell active-view="dashboard" title="Operator Overview" source="mock" :ready="true">
        <AdminDashboardView :state="mockState" :runs="mockRuns" :selected-detail="selectedDetail" />
      </OperatorAdminShell>
    `,
  }),
};

export const Runs: Story = {
  args: {
    activeView: "runs",
    title: "Runs",
    source: "mock",
    ready: true,
  },
  render: () => ({
    components: { AdminRunsView, OperatorAdminShell },
    setup() {
      return { defaultRunFilters, mockRuns };
    },
    template: `
      <OperatorAdminShell active-view="runs" title="Runs" source="mock" :ready="true">
        <AdminRunsView :filters="defaultRunFilters" :loading="false" :runs="mockRuns" selected-run-id="run-too-142-active" />
      </OperatorAdminShell>
    `,
  }),
};

export const RunDetail: Story = {
  args: {
    activeView: "runs",
    title: "Run Detail",
    source: "mock",
    ready: true,
  },
  render: () => ({
    components: { AdminRunDetailView, OperatorAdminShell },
    setup() {
      return { selectedDetail, selectedEvents };
    },
    template: `
      <OperatorAdminShell active-view="runs" title="Run Detail" source="mock" :ready="true">
        <AdminRunDetailView :detail="selectedDetail" :events="selectedEvents" />
      </OperatorAdminShell>
    `,
  }),
};
