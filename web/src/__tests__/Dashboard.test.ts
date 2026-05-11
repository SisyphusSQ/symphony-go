import { flushPromises, mount } from "@vue/test-utils";
import { defineComponent, h, type PropType } from "vue";
import { beforeEach, describe, expect, it, vi } from "vitest";

import type { RunRow } from "../api/operator";
import Dashboard from "../pages/Dashboard.vue";
import { mockOperatorApiClient, mockRunDetails, mockRuns, mockState } from "../fixtures/operator";

const ATableStub = defineComponent({
  props: {
    columns: { type: Array as PropType<Array<{ key: string }>>, required: true },
    customRow: { type: Function, required: false },
    dataSource: { type: Array as PropType<RunRow[]>, required: true },
    locale: { type: Object as PropType<{ emptyText?: string }>, required: false },
  },
  setup(props, { slots }) {
    return () =>
      h(
        "table",
        { "data-testid": "run-list-table" },
        props.dataSource.length === 0
          ? h("caption", props.locale?.emptyText || "empty")
          : h(
              "tbody",
              props.dataSource.map((record) =>
                h(
                  "tr",
                  props.customRow?.(record),
                  props.columns.map((column) =>
                    h("td", slots.bodyCell?.({ column, record }) ?? record.run_id),
                  ),
                ),
              ),
            ),
      );
  },
});

const ASelectStub = defineComponent({
  emits: ["change"],
  inheritAttrs: false,
  props: {
    value: { type: [Array, String] as PropType<string[] | string>, required: false },
    options: { type: Array as PropType<Array<{ label: string; value: string }>>, required: false },
    disabled: { type: Boolean, required: false },
  },
  setup(props, { emit }) {
    return () =>
      h(
        "div",
        (props.options ?? []).map((option) =>
          h(
            "button",
            {
              type: "button",
              onClick: () => emit("change", option.value),
            },
            option.label,
          ),
        ),
      );
  },
});

const antStubs = {
  "a-alert": {
    props: ["message"],
    template: `<div role="alert">{{ message }}</div>`,
  },
  "a-badge": {
    props: ["text"],
    template: `<span>{{ text }}</span>`,
  },
  "a-button": {
    template: `<button type="button" @click="$emit('click')"><slot name="icon" /><slot /></button>`,
  },
  "a-collapse": {
    template: `<section><slot /></section>`,
  },
  "a-collapse-panel": {
    props: ["header"],
    template: `<section><h3>{{ header }}</h3><slot /></section>`,
  },
  "a-descriptions": {
    template: `<dl><slot /></dl>`,
  },
  "a-descriptions-item": {
    props: ["label"],
    template: `<div><dt>{{ label }}</dt><dd><slot /></dd></div>`,
  },
  "a-empty": {
    props: ["description"],
    template: `<div>{{ description }}</div>`,
  },
  "a-input-search": {
    props: ["value"],
    template: `<input :value="value" @input="$emit('update:value', $event.target.value)" @keydown.enter="$emit('search')" />`,
  },
  "a-progress": {
    props: ["percent"],
    template: `<div data-testid="progress">{{ percent }}</div>`,
  },
  "a-select": ASelectStub,
  "a-spin": {
    template: `<div><slot /></div>`,
  },
  "a-statistic": {
    props: ["title", "value"],
    template: `<div><span>{{ title }}</span><strong>{{ value }}</strong></div>`,
  },
  "a-table": ATableStub,
  "a-tag": {
    template: `<span><slot /></span>`,
  },
  "a-timeline": {
    template: `<ol><slot /></ol>`,
  },
  "a-timeline-item": {
    template: `<li><slot /></li>`,
  },
};

describe("Dashboard", () => {
  beforeEach(() => {
    window.history.replaceState(null, "", "/");
  });

  it("loads state, run list, and selected run summary", async () => {
    const wrapper = mount(Dashboard, {
      props: {
        client: mockOperatorApiClient,
      },
      global: { stubs: antStubs },
    });

    await flushPromises();

    expect(wrapper.text()).toContain("Symphony Operator");
    expect(wrapper.text()).toContain("TOO-142");
    expect(wrapper.text()).toContain("run-too-142-active");
    expect(wrapper.text()).toContain("npm test running for dashboard shell");
    expect(wrapper.text()).toContain("Turn Timeline");
    expect(wrapper.text()).toContain("Turn started");
    expect(wrapper.text()).toContain("Raw redacted JSON");
    expect(wrapper.text()).toContain("Copy payload");
  });

  it("filters timeline categories for the selected run", async () => {
    const wrapper = mount(Dashboard, {
      props: {
        client: mockOperatorApiClient,
      },
      global: { stubs: antStubs },
    });

    await flushPromises();
    await wrapper.findAll("button").find((button) => button.text() === "tool")?.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("tool=linear_graphql success=true");
    expect(wrapper.text()).not.toContain("Turn started");
  });

  it("renders an empty timeline for runs without events", async () => {
    window.history.replaceState(null, "", "/runs/run-too-141-completed");

    const wrapper = mount(Dashboard, {
      props: {
        client: mockOperatorApiClient,
      },
      global: { stubs: antStubs },
    });

    await flushPromises();

    expect(wrapper.text()).toContain("run-too-141-completed");
    expect(wrapper.text()).toContain("No timeline events");
    expect(wrapper.text()).toContain("Select an event");
  });

  it("renders a detail error for an unknown run route", async () => {
    window.history.replaceState(null, "", "/runs/run-missing");

    const wrapper = mount(Dashboard, {
      props: {
        client: mockOperatorApiClient,
      },
      global: { stubs: antStubs },
    });

    await flushPromises();

    expect(wrapper.text()).toContain("mock run not found");
  });

  it("renders API errors without mock fallback", async () => {
    const client = {
      getState: vi.fn(async () => ({ data: mockState, source: "api" as const })),
      getRuns: vi.fn(async () => {
        throw new Error("operator API request failed");
      }),
      getRunDetail: vi.fn(async () => ({ data: mockRunDetails[mockRuns[0].run_id], source: "api" as const })),
      getRunEvents: vi.fn(async () => ({ data: { rows: [], limit: 100 }, source: "api" as const })),
    };

    const wrapper = mount(Dashboard, {
      props: { client },
      global: { stubs: antStubs },
    });

    await flushPromises();

    expect(wrapper.text()).toContain("operator API request failed");
    expect(wrapper.text()).toContain("No rows available");
  });
});
