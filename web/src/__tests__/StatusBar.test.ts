import { mount } from "@vue/test-utils";
import { describe, expect, it } from "vitest";

import StatusBar from "../components/StatusBar.vue";
import { mockState } from "../fixtures/operator";

const stubs = {
  "a-alert": {
    props: ["message"],
    template: `<div role="alert">{{ message }}</div>`,
  },
  "a-badge": {
    props: ["text"],
    template: `<span>{{ text }}</span>`,
  },
  "a-progress": {
    props: ["percent"],
    template: `<div data-testid="progress">{{ percent }}</div>`,
  },
  "a-statistic": {
    props: ["title", "value"],
    template: `<div><span>{{ title }}</span><strong>{{ value }}</strong></div>`,
  },
  "a-tag": {
    template: `<span><slot /></span>`,
  },
};

describe("StatusBar", () => {
  it("renders state counts and source label", () => {
    const wrapper = mount(StatusBar, {
      props: {
        state: mockState,
        loading: false,
        source: "mock",
        fallbackReason: "connect ECONNREFUSED",
      },
      global: { stubs },
    });

    expect(wrapper.text()).toContain("running");
    expect(wrapper.text()).toContain("Running");
    expect(wrapper.text()).toContain("Retrying");
    expect(wrapper.text()).toContain("Mock");
  });
});
