import { flushPromises, mount } from "@vue/test-utils";
import { defineComponent, h, type PropType } from "vue";
import { beforeEach, describe, expect, it } from "vitest";

import type { RunRow } from "../api/operator";
import { mockOperatorApiClient } from "../fixtures/operator";
import AdminApp from "../pages/AdminApp.vue";

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
  "a-select": ASelectStub,
  "a-spin": {
    template: `<div><slot /></div>`,
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

describe("AdminApp", () => {
  beforeEach(() => {
    window.history.replaceState(null, "", "/");
  });

  it("keeps dashboard refresh on the dashboard URL", async () => {
    window.history.replaceState(null, "", "/");

    const wrapper = mountAdminApp();
    await flushPromises();

    expect(wrapper.text()).toContain("Operator Overview");
    expect(window.location.pathname).toBe("/");
    expect(window.location.search).toBe("");
  });

  it("keeps runs refresh on the runs list URL", async () => {
    window.history.replaceState(null, "", "/runs");

    const wrapper = mountAdminApp();
    await flushPromises();

    expect(wrapper.text()).toContain("Read-only queue and history");
    expect(window.location.pathname).toBe("/runs");
    expect(window.location.search).toBe("");
  });

  it("opens run detail when refreshed on a run deep link", async () => {
    window.history.replaceState(null, "", "/runs/run-too-141-completed");

    const wrapper = mountAdminApp();
    await flushPromises();

    expect(wrapper.text()).toContain("Run Detail");
    expect(wrapper.text()).toContain("run-too-141-completed");
    expect(window.location.pathname).toBe("/runs/run-too-141-completed");
  });

  it("preserves explicit detail event and category query from a deep link", async () => {
    window.history.replaceState(
      null,
      "",
      "/runs/run-too-142-active?event_id=evt-142-4&category=tool",
    );

    const wrapper = mountAdminApp();
    await flushPromises();

    expect(wrapper.text()).toContain("tool=linear_graphql success=true");
    expect(window.location.pathname).toBe("/runs/run-too-142-active");
    expect(new URLSearchParams(window.location.search).get("event_id")).toBe("evt-142-4");
    expect(new URLSearchParams(window.location.search).get("category")).toBe("tool");
  });

  it("opens detail from the runs table without adding detail-only query", async () => {
    window.history.replaceState(null, "", "/runs");

    const wrapper = mountAdminApp();
    await flushPromises();
    await wrapper.find("tr").trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("Run Detail");
    expect(window.location.pathname).toBe("/runs/run-too-142-active");
    expect(window.location.search).toBe("");
  });

  it("clears detail query when navigating back to dashboard or runs", async () => {
    window.history.replaceState(
      null,
      "",
      "/runs/run-too-142-active?event_id=evt-142-4&category=tool",
    );

    const wrapper = mountAdminApp();
    await flushPromises();

    await navButton(wrapper, "Dashboard").trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("Operator Overview");
    expect(window.location.pathname).toBe("/");
    expect(window.location.search).toBe("");

    await navButton(wrapper, "Runs").trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("Read-only queue and history");
    expect(window.location.pathname).toBe("/runs");
    expect(window.location.search).toBe("");

    await wrapper.find("tr").trigger("click");
    await flushPromises();

    expect(window.location.pathname).toBe("/runs/run-too-142-active");
    expect(window.location.search).toBe("");
  });
});

function mountAdminApp() {
  return mount(AdminApp, {
    props: {
      client: mockOperatorApiClient,
    },
    global: { stubs: antStubs },
  });
}

function navButton(wrapper: ReturnType<typeof mountAdminApp>, label: string) {
  const button = wrapper.findAll("button").find((candidate) => candidate.text() === label);
  if (!button) {
    throw new Error(`missing nav button ${label}`);
  }
  return button;
}
