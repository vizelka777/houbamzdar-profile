const globalMapState = {
    pageSize: 200,
    offset: 0,
    hasMore: true,
    loaded: 0,
    mapped: 0,
    internalAdminView: false,
    isLoading: false,
    hasPendingReload: false,
    results: [],
    filters: {
        species: ""
    },
    map: null,
    markerLayer: null,
    bounds: null
};

const GLOBAL_MAP_NEARBY_RADIUS_METERS = 200;

function ensureGlobalMap() {
    const mapContainer = document.getElementById("global-map");
    if (!mapContainer || typeof L === "undefined") {
        return null;
    }

    if (!globalMapState.map) {
        globalMapState.map = L.map("global-map").setView([49.8, 15.5], 7);
        L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
            attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
        }).addTo(globalMapState.map);
        globalMapState.markerLayer = L.layerGroup().addTo(globalMapState.map);
        globalMapState.bounds = L.latLngBounds();
    }

    return globalMapState.map;
}

function readGlobalMapFiltersFromQuery() {
    const query = new URLSearchParams(window.location.search);
    globalMapState.filters.species = (query.get("species") || "").trim();
}

function syncGlobalMapFilterInputs() {
    const setValue = (id, value) => {
        const input = document.getElementById(id);
        if (input) {
            input.value = value;
        }
    };

    setValue("global-filter-species", globalMapState.filters.species);
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
}

function buildGlobalMapPopupHtml(capture) {
    const species = buildCaptureSpeciesLabel(capture);
    const region = buildCaptureRegionLabel(capture);

    return window.HZDMapUI.buildPopupHtml({
        authorName: capture.author_name || "Neznámý houbař",
        authorUrl: buildPublicProfileURL(capture.author_user_id),
        previewUrl: capture.public_url ? buildCaptureImageURL(capture, "popup") : "",
        altText: capture.author_name || "Neznámý houbař",
        dateValue: capture.captured_at,
        metaLines: [species, region],
        actionHtml: capture.public_url
            ? `<button type="button" class="btn btn-secondary map-popup-action global-map-open-btn" data-capture-id="${escapeHtml(capture.id)}">Otevřít ve fotkách</button>`
            : ""
    });
}

function degreesToRadians(value) {
    return (value * Math.PI) / 180;
}

function getDistanceBetweenCapturesMeters(origin, candidate) {
    const originLat = Number(origin?.latitude);
    const originLon = Number(origin?.longitude);
    const candidateLat = Number(candidate?.latitude);
    const candidateLon = Number(candidate?.longitude);
    if (
        Number.isNaN(originLat)
        || Number.isNaN(originLon)
        || Number.isNaN(candidateLat)
        || Number.isNaN(candidateLon)
    ) {
        return Number.POSITIVE_INFINITY;
    }

    const earthRadiusMeters = 6371000;
    const latDelta = degreesToRadians(candidateLat - originLat);
    const lonDelta = degreesToRadians(candidateLon - originLon);
    const lat1 = degreesToRadians(originLat);
    const lat2 = degreesToRadians(candidateLat);
    const haversine =
        Math.sin(latDelta / 2) * Math.sin(latDelta / 2)
        + Math.cos(lat1) * Math.cos(lat2) * Math.sin(lonDelta / 2) * Math.sin(lonDelta / 2);
    return 2 * earthRadiusMeters * Math.atan2(Math.sqrt(haversine), Math.sqrt(1 - haversine));
}

function buildNearbyGlobalMapCollection(captureID) {
    const targetCapture = globalMapState.results.find((capture) => capture && capture.id === captureID && capture.public_url);
    if (!targetCapture) {
        return [];
    }

    const nearby = globalMapState.results
        .filter((capture) => capture && capture.public_url)
        .map((capture) => ({
            capture,
            distanceMeters: capture.id === captureID
                ? 0
                : getDistanceBetweenCapturesMeters(targetCapture, capture)
        }))
        .filter((entry) => entry.capture.id === captureID || entry.distanceMeters <= GLOBAL_MAP_NEARBY_RADIUS_METERS)
        .sort((left, right) => {
            if (left.capture.id === captureID) return -1;
            if (right.capture.id === captureID) return 1;
            if (left.distanceMeters !== right.distanceMeters) {
                return left.distanceMeters - right.distanceMeters;
            }
            return String(right.capture.captured_at || "").localeCompare(String(left.capture.captured_at || ""));
        });

    return nearby.map((entry) => entry.capture);
}

function openGlobalMapCaptureLightbox(captureID) {
    const capturesToOpen = buildNearbyGlobalMapCollection(captureID);
    if (capturesToOpen.length === 0 || !window.HZDLightbox) {
        return;
    }

    window.HZDLightbox.openCollection(capturesToOpen, 0);
}

function applyGlobalMapMarkers(markers) {
    const map = ensureGlobalMap();
    if (!map) return;

    if (window.HZDMapClusters) {
        globalMapState.markerLayer = window.HZDMapClusters.replaceLayer(
            map,
            globalMapState.markerLayer,
            markers,
            {
                clusterOptions: {
                    maxClusterRadius: 58,
                    spiderfyDistanceMultiplier: 1.24
                }
            }
        );
        return;
    }

    if (!globalMapState.markerLayer || typeof globalMapState.markerLayer.clearLayers !== "function") {
        globalMapState.markerLayer = L.layerGroup().addTo(map);
    } else {
        globalMapState.markerLayer.clearLayers();
    }
    markers.forEach((marker) => marker.addTo(globalMapState.markerLayer));
}

function renderGlobalMapMarkers() {
    const map = ensureGlobalMap();
    if (!map) return;

    const markers = [];
    globalMapState.bounds = L.latLngBounds();

    globalMapState.results.forEach((capture) => {
        const lat = Number(capture.latitude);
        const lon = Number(capture.longitude);
        if (Number.isNaN(lat) || Number.isNaN(lon)) {
            return;
        }

        globalMapState.bounds.extend([lat, lon]);
        const marker = L.marker([lat, lon]);
        marker.on("click", () => {
            openGlobalMapCaptureLightbox(capture.id);
        });
        markers.push(marker);
    });

    globalMapState.mapped = markers.length;
    applyGlobalMapMarkers(markers);

    if (window.HZDMapClusters && globalMapState.markerLayer && markers.length > 0) {
        window.HZDMapClusters.fitLayer(map, globalMapState.markerLayer, { padding: [30, 30], maxZoom: 15 });
    } else if (globalMapState.bounds.isValid()) {
        map.fitBounds(globalMapState.bounds, { padding: [30, 30], maxZoom: 15 });
    } else {
        map.setView([49.8, 15.5], 7);
    }
}

async function loadGlobalMapBatch({ reset = false } = {}) {
    const map = ensureGlobalMap();
    if (!map) {
        return;
    }
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
        globalMapState.results = [];
        renderGlobalMapMarkers();
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
            globalMapState.offset = globalMapState.loaded;
            globalMapState.internalAdminView = globalMapState.internalAdminView || Boolean(result.internal_admin_view);
            globalMapState.hasMore = result.captures.length === globalMapState.pageSize;
            renderGlobalMapMarkers();
            updateGlobalMapSummary();

            offset += result.captures.length;
            hasMore = result.captures.length === globalMapState.pageSize;
        }
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
    const readValue = (id) => (document.getElementById(id)?.value || "").trim();

    globalMapState.filters.species = readValue("global-filter-species");
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
    if (!ensureGlobalMap()) return;

    readGlobalMapFiltersFromQuery();
    syncGlobalMapFilterInputs();

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

    await loadGlobalMapBatch({ reset: true });
});
