const globalMapState = {
    pageSize: 200,
    offset: 0,
    hasMore: true,
    loaded: 0,
    mapped: 0,
    internalAdminView: false,
    isLoading: false,
    hasPendingReload: false,
    hasLoadedOnce: false,
    results: [],
    filters: {
        species: ""
    }
};

const GLOBAL_MAP_NEARBY_RADIUS_METERS = 200;

function readGlobalMapFiltersFromQuery() {
    const query = new URLSearchParams(window.location.search);
    globalMapState.filters.species = (query.get("species") || "").trim();
}

function syncGlobalMapFilterInputs() {
    const input = document.getElementById("global-filter-species");
    if (input) {
        input.value = globalMapState.filters.species;
    }
}

function syncGlobalMapQueryString() {
    const params = new URLSearchParams();
    if (globalMapState.filters.species) params.set("species", globalMapState.filters.species);

    const query = params.toString();
    const nextURL = query ? `${window.location.pathname}?${query}` : window.location.pathname;
    window.history.replaceState({}, "", nextURL);
}

function buildGlobalMapQuery(offset = globalMapState.offset) {
    const params = new URLSearchParams();
    params.set("limit", String(globalMapState.pageSize));
    params.set("offset", String(offset));
    if (globalMapState.filters.species) params.set("species", globalMapState.filters.species);
    return params.toString();
}

function currentGlobalMapCaptures() {
    return globalMapState.results.filter((capture) => captureHasCoordinates(capture));
}

function syncGlobalMapLaunchState() {
    const button = document.getElementById("global-map-open-viewer-btn");
    if (!button) {
        return;
    }

    const captures = currentGlobalMapCaptures();
    button.disabled = globalMapState.isLoading || captures.length === 0;
    button.textContent = globalMapState.isLoading ? "Načítám mapu..." : "Otevřít mapu na celou obrazovku";
}

function updateGlobalMapSummary() {
    const summary = document.getElementById("global-map-summary");
    if (summary) {
        if (globalMapState.isLoading && globalMapState.loaded === 0) {
            summary.textContent = "Načítám body na mapě...";
        } else if (globalMapState.isLoading) {
            summary.textContent = `Načítám body na mapě... zatím ${globalMapState.loaded}.`;
        } else if (globalMapState.loaded === 0) {
            summary.textContent = "Pro tento filtr zatím není žádný bod na mapě.";
        } else if (globalMapState.internalAdminView) {
            summary.textContent = `Načteno ${globalMapState.loaded} bodů v interním admin režimu.`;
        } else {
            summary.textContent = `Načteno ${globalMapState.loaded} bodů na mapě.`;
        }
    }

    syncGlobalMapLaunchState();
}

function openGlobalMapCaptureLightbox(captureID) {
    window.HZDMapUI?.openLightboxCollection(globalMapState.results, captureID, {
        nearby: true,
        requirePublicUrl: true,
        radiusMeters: GLOBAL_MAP_NEARBY_RADIUS_METERS
    });
}

async function openGlobalMapViewer() {
    if (!globalMapState.hasLoadedOnce && !globalMapState.isLoading) {
        await loadGlobalMapBatch({ reset: true });
    }

    const captures = currentGlobalMapCaptures();
    if (!captures.length || !window.HZDMapUI?.openViewer) {
        syncGlobalMapLaunchState();
        return false;
    }

    const summary = document.getElementById("global-map-summary")?.textContent?.trim() || "";
    return window.HZDMapUI.openViewer(captures, null, {
        title: "Veřejná mapa",
        note: summary,
        onCaptureActivate: (capture) => {
            openGlobalMapCaptureLightbox(capture.id);
        }
    });
}

window.openGlobalMapViewer = openGlobalMapViewer;

async function loadGlobalMapBatch({ reset = false } = {}) {
    if (globalMapState.isLoading) {
        globalMapState.hasPendingReload = globalMapState.hasPendingReload || reset;
        return;
    }

    globalMapState.isLoading = true;
    if (reset) {
        globalMapState.offset = 0;
        globalMapState.hasMore = true;
        globalMapState.loaded = 0;
        globalMapState.mapped = 0;
        globalMapState.internalAdminView = false;
        globalMapState.hasLoadedOnce = false;
        globalMapState.results = [];
        updateGlobalMapSummary();
    }

    try {
        let offset = 0;
        let hasMore = true;
        const capturesByID = new Map();

        while (hasMore) {
            const result = await apiGet(`/api/public/map-captures?${buildGlobalMapQuery(offset)}`);
            if (!result || !result.ok || !Array.isArray(result.captures)) {
                throw new Error("Nepodařilo se načíst body na mapě.");
            }

            result.captures.forEach((capture) => {
                if (capture?.id) {
                    capturesByID.set(capture.id, capture);
                }
            });

            globalMapState.results = Array.from(capturesByID.values());
            globalMapState.loaded = globalMapState.results.length;
            globalMapState.mapped = currentGlobalMapCaptures().length;
            globalMapState.offset = globalMapState.loaded;
            globalMapState.internalAdminView = globalMapState.internalAdminView || Boolean(result.internal_admin_view);
            globalMapState.hasMore = result.captures.length === globalMapState.pageSize;
            updateGlobalMapSummary();

            offset += result.captures.length;
            hasMore = result.captures.length === globalMapState.pageSize;
        }

        globalMapState.hasLoadedOnce = true;
    } catch (error) {
        console.error("Failed to load global map batch", error);
        const summary = document.getElementById("global-map-summary");
        if (summary) {
            summary.textContent = error.message || "Mapa se nepodařila načíst.";
        }
    } finally {
        globalMapState.isLoading = false;
        updateGlobalMapSummary();
        if (globalMapState.hasPendingReload) {
            globalMapState.hasPendingReload = false;
            loadGlobalMapBatch({ reset: true });
        }
    }
}

function readGlobalMapFiltersFromForm() {
    globalMapState.filters.species = (document.getElementById("global-filter-species")?.value || "").trim();
}

function resetGlobalMapFilters() {
    globalMapState.filters = {
        species: ""
    };
    syncGlobalMapFilterInputs();
    syncGlobalMapQueryString();
    return loadGlobalMapBatch({ reset: true });
}

document.addEventListener("DOMContentLoaded", async () => {
    if (document.body.dataset.page !== "map") return;

    readGlobalMapFiltersFromQuery();
    syncGlobalMapFilterInputs();
    syncGlobalMapLaunchState();

    const filterForm = document.getElementById("global-map-filter-form");
    if (filterForm) {
        filterForm.addEventListener("submit", async (event) => {
            event.preventDefault();
            readGlobalMapFiltersFromForm();
            syncGlobalMapQueryString();
            const filtersDetails = document.getElementById("global-map-filters");
            if (filtersDetails && window.innerWidth <= 700) {
                filtersDetails.open = false;
            }
            await loadGlobalMapBatch({ reset: true });
        });
    }

    const resetButton = document.getElementById("global-map-filter-reset");
    if (resetButton) {
        resetButton.addEventListener("click", async () => {
            const filtersDetails = document.getElementById("global-map-filters");
            if (filtersDetails && window.innerWidth <= 700) {
                filtersDetails.open = false;
            }
            await resetGlobalMapFilters();
        });
    }

    const openButton = document.getElementById("global-map-open-viewer-btn");
    if (openButton) {
        openButton.addEventListener("click", async () => {
            await openGlobalMapViewer();
        });
    }

    await loadGlobalMapBatch({ reset: true });
});
