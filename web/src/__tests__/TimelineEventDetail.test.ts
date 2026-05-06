import { mount } from "@vue/test-utils";
import { describe, expect, it, vi } from "vitest";

import TimelineEventDetail from "../components/TimelineEventDetail.vue";
import { mockRunEvents } from "../fixtures/operator";

const antStubs = {
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
  "a-tag": {
    template: `<span><slot /></span>`,
  },
};

describe("TimelineEventDetail", () => {
  it("shows raw redacted JSON and copies the current payload", async () => {
    const writeText = vi.fn(async () => {});
    Object.defineProperty(navigator, "clipboard", {
      configurable: true,
      value: { writeText },
    });
    const event = mockRunEvents["run-too-142-active"][0];

    const wrapper = mount(TimelineEventDetail, {
      props: { event },
      global: { stubs: antStubs },
    });

    expect(wrapper.text()).toContain("Raw redacted JSON");
    expect(wrapper.text()).toContain("turn_started");

    await wrapper.find("button").trigger("click");

    expect(writeText).toHaveBeenCalledWith(JSON.stringify(event.payload, null, 2));
    expect(wrapper.text()).toContain("Copied");
  });
});
