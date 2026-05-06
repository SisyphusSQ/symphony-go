import type { RunStatus, RunTimelineEvent, TokenTotals } from "./operator";

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

export function formatDurationMS(milliseconds: number | undefined): string {
  if (milliseconds === undefined || milliseconds < 0) {
    return "";
  }
  if (milliseconds < 1000) {
    return `${Math.round(milliseconds)}ms`;
  }
  return formatRuntime(milliseconds / 1000);
}

export function eventDurationMS(event: RunTimelineEvent): number | undefined {
  return (
    event.duration_ms ??
    readNumber(event.payload, "duration_ms") ??
    readNumber(event.payload, "durationMs") ??
    readNumber(event.payload, "runtime_ms")
  );
}

export function formatEventTokens(event: RunTimelineEvent): string {
  const total =
    event.token_totals?.total_tokens ??
    readNumber(event.payload, "token_totals.total_tokens") ??
    readNumber(event.payload, "usage.total_tokens") ??
    readNumber(event.payload, "total_tokens");
  return total === undefined ? "" : formatCount(total);
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

function readNumber(value: unknown, path: string): number | undefined {
  const found = path.split(".").reduce<unknown>((current, key) => {
    if (!current || typeof current !== "object") {
      return undefined;
    }
    return (current as Record<string, unknown>)[key];
  }, value);
  return typeof found === "number" && Number.isFinite(found) ? found : undefined;
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
