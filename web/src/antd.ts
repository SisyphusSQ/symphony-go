import {
  Alert,
  Badge,
  Button,
  Collapse,
  ConfigProvider,
  Descriptions,
  Empty,
  Input,
  Progress,
  Select,
  Spin,
  Statistic,
  Table,
  Tag,
  Timeline,
} from "ant-design-vue";
import type { App } from "vue";

const components = [
  Alert,
  Badge,
  Button,
  Collapse,
  ConfigProvider,
  Descriptions,
  Empty,
  Input,
  Progress,
  Select,
  Spin,
  Statistic,
  Table,
  Tag,
  Timeline,
];

export function registerAntd(app: App): void {
  components.forEach((component) => {
    app.use(component);
  });
}
