/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_OPERATOR_API_BASE?: string;
  readonly VITE_OPERATOR_PROXY_TARGET?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
