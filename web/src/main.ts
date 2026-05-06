import {
  Alert,
  Badge,
  Button,
  ConfigProvider,
  Descriptions,
  Empty,
  Input,
  Select,
  Spin,
  Statistic,
  Table,
  Tag,
} from "ant-design-vue";
import "ant-design-vue/dist/reset.css";
import { createApp } from "vue";

import App from "./App.vue";
import "./styles.css";

const app = createApp(App);

[
  Alert,
  Badge,
  Button,
  ConfigProvider,
  Descriptions,
  Empty,
  Input,
  Select,
  Spin,
  Statistic,
  Table,
  Tag,
].forEach((component) => {
  app.use(component);
});

app.mount("#app");
