import "ant-design-vue/dist/reset.css";
import { createApp } from "vue";

import { registerAntd } from "./antd";
import App from "./App.vue";
import "./styles.css";

const app = createApp(App);

registerAntd(app);

app.mount("#app");
