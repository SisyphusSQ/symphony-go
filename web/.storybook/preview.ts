import type { Preview } from "@storybook/vue3-vite";
import { setup } from "@storybook/vue3-vite";
import "ant-design-vue/dist/reset.css";

import { registerAntd } from "../src/antd";
import "../src/styles.css";
import { operatorTheme } from "../src/theme";

setup((app) => {
  registerAntd(app);
});

const preview: Preview = {
  decorators: [
    (story) => ({
      components: { story },
      setup() {
        return { operatorTheme };
      },
      template: '<a-config-provider :theme="operatorTheme"><story /></a-config-provider>',
    }),
  ],
  parameters: {
    controls: {
      matchers: {
        color: /(background|color)$/i,
        date: /Date$/i,
      },
    },
    layout: "fullscreen",
  },
};

export default preview;
