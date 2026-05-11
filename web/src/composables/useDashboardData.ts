import { computed, ref } from "vue";

import {
  createFetchOperatorApiClient,
  defaultRunEventFilters,
  defaultRunFilters,
  errorMessage,
  isTimelineCategory,
  type OperatorApiClient,
  type OperatorDataSource,
  type RunDetail,
  type RunEventFilters,
  type RunEventPage,
  type RunFilters,
  type RunPage,
  type RunRow,
  type RunTimelineEvent,
  type StateResponse,
  type TimelineCategoryFilter,
} from "../api/operator";

export interface DashboardLoadState {
  state: StateResponse | null;
  runs: RunRow[];
  selectedDetail: RunDetail | null;
  selectedEvents: RunTimelineEvent[];
  timelineEvents: RunTimelineEvent[];
  selectedEvent: RunTimelineEvent | null;
  loading: boolean;
  detailLoading: boolean;
  eventsLoading: boolean;
  error: string;
  detailError: string;
  eventsError: string;
  source: OperatorDataSource;
  fallbackReason: string;
  filters: RunFilters;
  eventFilters: RunEventFilters;
}

export function createDefaultOperatorClient(): OperatorApiClient {
  return createFetchOperatorApiClient();
}

export function useDashboardData(client: OperatorApiClient = createDefaultOperatorClient()) {
  const state = ref<StateResponse | null>(null);
  const runsPage = ref<RunPage | null>(null);
  const selectedDetail = ref<RunDetail | null>(null);
  const selectedRunID = ref("");
  const eventsPage = ref<RunEventPage | null>(null);
  const selectedEventID = ref("");
  const loading = ref(false);
  const detailLoading = ref(false);
  const eventsLoading = ref(false);
  const error = ref("");
  const detailError = ref("");
  const eventsError = ref("");
  const source = ref<OperatorDataSource>("api");
  const fallbackReason = ref("");
  const filters = ref<RunFilters>({ ...defaultRunFilters });
  const eventFilters = ref<RunEventFilters>(initialEventFilters());
  const requestedEventID = ref(initialSelection().eventID);
  let detailRequestID = 0;

  const runs = computed(() => runsPage.value?.rows ?? []);
  const timelineEvents = computed(() => eventsPage.value?.rows ?? []);
  const selectedEvents = timelineEvents;
  const selectedEvent = computed(() => {
    if (!selectedEventID.value) {
      return null;
    }
    return timelineEvents.value.find((event) => event.id === selectedEventID.value) ?? null;
  });
  const isEmpty = computed(() => !loading.value && runs.value.length === 0);

  async function loadDashboard(nextFilters: RunFilters = filters.value) {
    filters.value = {
      statuses: [...nextFilters.statuses],
      issue: nextFilters.issue,
      limit: nextFilters.limit,
    };
    loading.value = true;
    error.value = "";
    fallbackReason.value = "";
    try {
      const [stateResult, runResult] = await Promise.all([
        client.getState(),
        client.getRuns(filters.value),
      ]);
      state.value = stateResult.data;
      runsPage.value = runResult.data;
      source.value = stateResult.source === "mock" || runResult.source === "mock" ? "mock" : "api";
      fallbackReason.value = stateResult.fallbackReason || runResult.fallbackReason || "";

      const routeRunID = initialSelection().runID;
      const selectedStillVisible = runs.value.some((run) => run.run_id === selectedRunID.value);
      const nextSelection = selectedStillVisible
        ? selectedRunID.value
        : routeRunID || runs.value[0]?.run_id || "";
      if (nextSelection) {
        await selectRun(nextSelection);
      } else {
        selectedRunID.value = "";
        selectedDetail.value = null;
        eventsPage.value = null;
        selectedEventID.value = "";
      }
    } catch (loadError) {
      error.value = errorMessage(loadError);
      state.value = null;
      runsPage.value = null;
      selectedRunID.value = "";
      selectedDetail.value = null;
      eventsPage.value = null;
      selectedEventID.value = "";
    } finally {
      loading.value = false;
    }
  }

  async function selectRun(runID: string) {
    const cleanRunID = runID.trim();
    const requestID = ++detailRequestID;
    selectedRunID.value = cleanRunID;
    selectedDetail.value = null;
    eventsPage.value = null;
    selectedEventID.value = "";
    detailError.value = "";
    eventsError.value = "";
    if (!cleanRunID) {
      detailLoading.value = false;
      eventsLoading.value = false;
      syncLocation();
      return;
    }
    detailLoading.value = true;
    eventsLoading.value = true;
    try {
      const [detailResult, eventsResult] = await Promise.allSettled([
        client.getRunDetail(cleanRunID),
        client.getRunEvents(cleanRunID, eventFilters.value),
      ]);
      if (requestID !== detailRequestID) {
        return;
      }

      if (detailResult.status === "fulfilled") {
        selectedDetail.value = detailResult.value.data;
        if (detailResult.value.source === "mock") {
          source.value = "mock";
          fallbackReason.value = detailResult.value.fallbackReason || fallbackReason.value;
        }
      } else {
        detailError.value = errorMessage(detailResult.reason);
      }

      if (eventsResult.status === "fulfilled") {
        eventsPage.value = eventsResult.value.data;
        if (eventsResult.value.source === "mock") {
          source.value = "mock";
          fallbackReason.value = eventsResult.value.fallbackReason || fallbackReason.value;
        }
        selectRequestedOrFirstEvent();
      } else {
        eventsError.value = errorMessage(eventsResult.reason);
      }
      syncLocation();
    } finally {
      if (requestID === detailRequestID) {
        detailLoading.value = false;
        eventsLoading.value = false;
      }
    }
  }

  async function loadRunEvents(
    runID: string = selectedRunID.value,
    nextFilters: RunEventFilters = eventFilters.value,
  ) {
    const cleanRunID = runID.trim();
    eventFilters.value = { ...nextFilters };
    eventsError.value = "";
    eventsPage.value = null;
    selectedEventID.value = "";
    if (!cleanRunID) {
      syncLocation();
      return;
    }
    eventsLoading.value = true;
    try {
      const result = await client.getRunEvents(cleanRunID, eventFilters.value);
      if (cleanRunID !== selectedRunID.value) {
        return;
      }
      eventsPage.value = result.data;
      if (result.source === "mock") {
        source.value = "mock";
        fallbackReason.value = result.fallbackReason || fallbackReason.value;
      }
      selectRequestedOrFirstEvent();
      syncLocation();
    } catch (eventLoadError) {
      eventsError.value = errorMessage(eventLoadError);
      syncLocation();
    } finally {
      eventsLoading.value = false;
    }
  }

  function updateEventCategory(category: TimelineCategoryFilter) {
    const safeCategory = category === "all" || isTimelineCategory(category) ? category : "all";
    requestedEventID.value = "";
    return loadRunEvents(selectedRunID.value, {
      ...eventFilters.value,
      category: safeCategory,
    });
  }

  function selectEvent(eventID: string) {
    selectedEventID.value = eventID;
    requestedEventID.value = eventID;
    syncLocation();
  }

  function updateFilters(nextFilters: RunFilters) {
    return loadDashboard(nextFilters);
  }

  function selectRequestedOrFirstEvent() {
    const requested = requestedEventID.value;
    const visibleRequested = timelineEvents.value.find((event) => event.id === requested);
    selectedEventID.value = visibleRequested?.id || timelineEvents.value[0]?.id || "";
    requestedEventID.value = "";
  }

  function syncLocation() {
    if (typeof window === "undefined" || !window.history?.replaceState) {
      return;
    }
    const url = new URL(window.location.href);
    if (selectedRunID.value) {
      url.pathname = `/runs/${encodeURIComponent(selectedRunID.value)}`;
      url.searchParams.set("run_id", selectedRunID.value);
    } else {
      url.pathname = "/";
      url.searchParams.delete("run_id");
    }
    if (selectedEventID.value) {
      url.searchParams.set("event_id", selectedEventID.value);
    } else {
      url.searchParams.delete("event_id");
    }
    if (eventFilters.value.category !== "all") {
      url.searchParams.set("category", eventFilters.value.category);
    } else {
      url.searchParams.delete("category");
    }
    window.history.replaceState(null, "", `${url.pathname}${url.search}${url.hash}`);
  }

  return {
    state,
    runs,
    selectedDetail,
    selectedEvents,
    selectedRunID,
    timelineEvents,
    selectedEvent,
    selectedEventID,
    loading,
    detailLoading,
    eventsLoading,
    error,
    detailError,
    eventsError,
    source,
    fallbackReason,
    filters,
    eventFilters,
    isEmpty,
    loadDashboard,
    loadRunEvents,
    selectRun,
    selectEvent,
    updateFilters,
    updateEventCategory,
  };
}

function initialSelection(): { runID: string; eventID: string } {
  if (typeof window === "undefined") {
    return { runID: "", eventID: "" };
  }
  const params = new URLSearchParams(window.location.search);
  const pathMatch = window.location.pathname.match(/^\/runs\/([^/]+)$/);
  return {
    runID: params.get("run_id") || (pathMatch ? decodeURIComponent(pathMatch[1]) : ""),
    eventID: params.get("event_id") || "",
  };
}

function initialEventFilters(): RunEventFilters {
  if (typeof window === "undefined") {
    return { ...defaultRunEventFilters };
  }
  const rawCategory = new URLSearchParams(window.location.search).get("category") || "all";
  return {
    ...defaultRunEventFilters,
    category: rawCategory === "all" || isTimelineCategory(rawCategory) ? rawCategory : "all",
  };
}
