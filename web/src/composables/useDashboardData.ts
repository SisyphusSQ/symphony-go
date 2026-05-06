import { computed, ref } from "vue";

import {
  createFetchOperatorApiClient,
  defaultRunFilters,
  errorMessage,
  type OperatorApiClient,
  type OperatorDataSource,
  type RunDetail,
  type RunFilters,
  type RunPage,
  type RunRow,
  type StateResponse,
} from "../api/operator";
import { mockOperatorApiClient } from "../fixtures/operator";

export interface DashboardLoadState {
  state: StateResponse | null;
  runs: RunRow[];
  selectedDetail: RunDetail | null;
  loading: boolean;
  detailLoading: boolean;
  error: string;
  detailError: string;
  source: OperatorDataSource;
  fallbackReason: string;
  filters: RunFilters;
}

export function createDefaultOperatorClient(): OperatorApiClient {
  return createFetchOperatorApiClient({
    fallback: mockOperatorApiClient,
  });
}

export function useDashboardData(client: OperatorApiClient = createDefaultOperatorClient()) {
  const state = ref<StateResponse | null>(null);
  const runsPage = ref<RunPage | null>(null);
  const selectedDetail = ref<RunDetail | null>(null);
  const selectedRunID = ref("");
  const loading = ref(false);
  const detailLoading = ref(false);
  const error = ref("");
  const detailError = ref("");
  const source = ref<OperatorDataSource>("api");
  const fallbackReason = ref("");
  const filters = ref<RunFilters>({ ...defaultRunFilters });

  const runs = computed(() => runsPage.value?.rows ?? []);
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

      const selectedStillVisible = runs.value.some((run) => run.run_id === selectedRunID.value);
      const nextSelection = selectedStillVisible ? selectedRunID.value : runs.value[0]?.run_id || "";
      if (nextSelection) {
        await selectRun(nextSelection);
      } else {
        selectedRunID.value = "";
        selectedDetail.value = null;
      }
    } catch (loadError) {
      error.value = errorMessage(loadError);
      state.value = null;
      runsPage.value = null;
      selectedRunID.value = "";
      selectedDetail.value = null;
    } finally {
      loading.value = false;
    }
  }

  async function selectRun(runID: string) {
    const cleanRunID = runID.trim();
    selectedRunID.value = cleanRunID;
    selectedDetail.value = null;
    detailError.value = "";
    if (!cleanRunID) {
      return;
    }
    detailLoading.value = true;
    try {
      const result = await client.getRunDetail(cleanRunID);
      selectedDetail.value = result.data;
      if (result.source === "mock") {
        source.value = "mock";
        fallbackReason.value = result.fallbackReason || fallbackReason.value;
      }
    } catch (detailLoadError) {
      detailError.value = errorMessage(detailLoadError);
    } finally {
      detailLoading.value = false;
    }
  }

  function updateFilters(nextFilters: RunFilters) {
    return loadDashboard(nextFilters);
  }

  return {
    state,
    runs,
    selectedDetail,
    selectedRunID,
    loading,
    detailLoading,
    error,
    detailError,
    source,
    fallbackReason,
    filters,
    isEmpty,
    loadDashboard,
    selectRun,
    updateFilters,
  };
}
