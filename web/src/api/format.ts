import type { RunStatus, TokenTotals } from "./operator";

export function formatCount(value: number | undefined): string {
  return new Intl.NumberFormat("en-US").format(value ?? 0);
}

export function formatTokens(tokens: TokenTotals | undefined): string {
  return formatCount(tokens?.total_tokens);
}

export function formatRuntime(seconds: number | undefined): string {
  const total = Math.max(0, Math.floor(seconds ?? 0));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const rest = total % 60;
  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }
  if (minutes > 0) {
    return `${minutes}m ${rest}s`;
  }
  return `${rest}s`;
}

export function formatDateTime(value: string | undefined): string {
  if (!value) {
    return "-";
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return new Intl.DateTimeFormat("en-US", {
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

export function statusTone(status: string): "success" | "processing" | "warning" | "error" | "default" {
  switch (status as RunStatus) {
    case "running":
      return "processing";
    case "retrying":
      return "warning";
    case "completed":
      return "success";
    case "failed":
      return "error";
    default:
      return "default";
  }
}

export function tagColor(status: string): string {
  switch (status as RunStatus) {
    case "running":
      return "blue";
    case "retrying":
      return "gold";
    case "completed":
      return "green";
    case "failed":
      return "red";
    case "stopped":
      return "purple";
    case "interrupted":
      return "volcano";
    default:
      return "default";
  }
}
