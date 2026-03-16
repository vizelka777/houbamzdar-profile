const globalMapState = {
    pageSize: 120,
    offset: 0,
    hasMore: true,
    loaded: 0,
    mapped: 0,
    isLoading: false,
    results: [],
    filters: {
        species: "",
        kraj: "",
        okres: "",
        obec: "",
        sort: "published_desc"
    },
    map: null,
    markerLayer: null,
    bounds: null
};

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
    globalMapState.filters.kraj = (query.get("kraj") || "").trim();
    globalMapState.filters.okres = (query.get("okres") || "").trim();
    globalMapState.filters.obec = (query.get("obec") || "").trim();
    globalMapState.filters.sort = (query.get("sort") || "published_desc").trim() || "published_desc";
}

function syncGlobalMapFilterInputs() {
    const setValue = (id, value) => {
        const input = document.getElementById(id);
        if (input) {
            input.value = value;
        }
    };

    setValue("global-filter-species", globalMapState.filters.species);
    setValue("global-filter-kraj", globalMapState.filters.kraj);
    setValue("global-filter-okres", globalMapState.filters.okres);
    setValue("global-filter-obec", globalMapState.filters.obec);
    setValue("global-filter-sort", globalMapState.filters.sort);
}

function syncGlobalMapQueryString() {
    const params = new URLSearchParams();
    if (globalMapState.filters.species) params.set("species", globalMapState.filters.species);
    if (globalMapState.filters.kraj) params.set("kraj", globalMapState.filters.kraj);
    if (globalMapState.filters.okres) params.set("okres", globalMapState.filters.okres);
    if (globalMapState.filters.obec) params.set("obec", globalMapState.filters.obec);
    if (globalMapState.filters.sort && globalMapState.filters.sort !== "published_desc") {
        params.set("sort", globalMapState.filters.sort);
    }

    const query = params.toString();
    const nextURL = query ? `${window.location.pathname}?${query}` : window.location.pathname;
    window.history.replaceState({}, "", nextURL);
}

function buildGlobalMapQuery() {
    const params = new URLSearchParams();
    params.set("limit", String(globalMapState.pageSize));
    params.set("offset", String(globalMapState.offset));
    if (globalMapState.filters.species) params.set("species", globalMapState.filters.species);
    if (globalMapState.filters.kraj) params.set("kraj", globalMapState.filters.kraj);
    if (globalMapState.filters.okres) params.set("okres", globalMapState.filters.okres);
    if (globalMapState.filters.obec) params.set("obec", globalMapState.filters.obec);
    if (globalMapState.filters.sort) params.set("sort", globalMapState.filters.sort);
    return params.toString();
}

function updateGlobalMapSummary() {
    const summary = document.getElementById("global-map-summary");
    const loadMoreButton = document.getElementById("global-map-load-more-btn");
    if (summary) {
        if (globalMapState.isLoading && globalMapState.loaded === 0) {
            summary.textContent = "Načítám veřejné fotografie...";
        } else if (globalMapState.loaded === 0) {
            summary.textContent = "Pro tento filtr zatím není žádná veřejná fotografie.";
        } else {
            summary.textContent = `Načteno ${globalMapState.loaded} veřejných fotografií, na mapě ${globalMapState.mapped} s dostupnou polohou.`;
        }
    }
    if (loadMoreButton) {
        loadMoreButton.style.display = globalMapState.hasMore ? "inline-flex" : "none";
        loadMoreButton.disabled = globalMapState.isLoading;
    }
}

function buildGlobalMapPopupHtml(capture) {
    const author = escapeHtml(capture.author_name || "Neznámý houbař");
    const authorUrl = escapeHtml(buildPublicProfileURL(capture.author_user_id));
    const date = escapeHtml(formatDateTime(capture.captured_at));
    const imageHtml = capture.public_url
        ? `<a href="${escapeHtml(buildCaptureImageURL(capture, "original"))}" target="_blank" rel="noreferrer"><img src="${escapeHtml(buildCaptureImageURL(capture, "popup"))}" alt="${author}" loading="lazy"></a>`
        : '<div class="map-popup-placeholder">Bez veřejného náhledu</div>';
    const species = buildCaptureSpeciesLabel(capture);
    const region = buildCaptureRegionLabel(capture);

    return `
        <div class="map-popup-content">
            ${imageHtml}
            <h4><a href="${authorUrl}">${author}</a></h4>
            <p>${date}</p>
            ${species ? `<p>${escapeHtml(species)}</p>` : ""}
            ${region ? `<p>${escapeHtml(region)}</p>` : ""}
        </div>
    `;
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
        marker.bindPopup(buildGlobalMapPopupHtml(capture));
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

function buildGlobalMapResultCard(capture) {
    const authorName = escapeHtml(capture.author_name || "Neznámý houbař");
    const authorURL = escapeHtml(buildPublicProfileURL(capture.author_user_id));
    const previewURL = escapeHtml(buildCaptureImageURL(capture, "thumb"));
    const fullURL = escapeHtml(buildCaptureImageURL(capture, "original"));
    const date = escapeHtml(formatDateTime(capture.captured_at));
    const species = buildCaptureSpeciesLabel(capture);
    const region = buildCaptureRegionLabel(capture);
    const regionNote = buildCaptureRegionSearchNote(capture);
    const exactLocationNote = captureHasCoordinates(capture)
        ? "Poloha je dostupná i na mapě."
        : "Přesná poloha zůstává skrytá, veřejně je vidět jen oblast.";

    return `
        <article class="public-capture-result-card">
            <a href="${fullURL}" target="_blank" rel="noreferrer" class="public-capture-result-thumb-link">
                <img src="${previewURL}" alt="${authorName}" class="public-capture-result-thumb" loading="lazy">
                ${buildCaptureAccessBadgeHtml(capture)}
            </a>
            <div class="public-capture-result-body">
                <div class="public-capture-result-topline">
                    <a href="${authorURL}" class="author-link public-capture-result-author">${authorName}</a>
                    <span class="public-capture-result-date">${date}</span>
                </div>
                ${species ? `<strong class="public-capture-result-species">${escapeHtml(species)}</strong>` : ""}
                ${region ? `<p class="public-capture-result-copy">${escapeHtml(region)}</p>` : ""}
                ${regionNote ? `<p class="public-capture-result-copy">${escapeHtml(regionNote)}</p>` : ""}
                <p class="public-capture-result-copy">${escapeHtml(exactLocationNote)}</p>
            </div>
        </article>
    `;
}

function renderGlobalMapResults() {
    const resultsNode = document.getElementById("global-map-results");
    if (!resultsNode) return;

    if (!globalMapState.results.length) {
        resultsNode.innerHTML = "";
        return;
    }

    resultsNode.innerHTML = globalMapState.results.map(buildGlobalMapResultCard).join("");
}

async function loadGlobalMapBatch({ reset = false } = {}) {
    const map = ensureGlobalMap();
    if (!map || globalMapState.isLoading || (!reset && !globalMapState.hasMore)) {
        return;
    }

    globalMapState.isLoading = true;
    if (reset) {
        globalMapState.offset = 0;
        globalMapState.hasMore = true;
        globalMapState.loaded = 0;
        globalMapState.mapped = 0;
        globalMapState.results = [];
        renderGlobalMapResults();
        updateGlobalMapSummary();
    }

    try {
        const result = await apiGet(`/api/public/captures?${buildGlobalMapQuery()}`);
        if (!result || !result.ok || !Array.isArray(result.captures)) {
            throw new Error("Nepodařilo se načíst veřejné fotografie.");
        }

        globalMapState.results = reset
            ? result.captures.slice()
            : globalMapState.results.concat(result.captures);
        globalMapState.loaded = globalMapState.results.length;
        globalMapState.offset += result.captures.length;
        globalMapState.hasMore = result.captures.length === globalMapState.pageSize;

        renderGlobalMapMarkers();
        renderGlobalMapResults();
    } catch (error) {
        console.error("Failed to load global map batch", error);
        const summary = document.getElementById("global-map-summary");
        if (summary) {
            summary.textContent = error.message || "Mapa se nepodařila načíst.";
        }
    } finally {
        globalMapState.isLoading = false;
        updateGlobalMapSummary();
    }
}

function readGlobalMapFiltersFromForm() {
    const readValue = (id) => (document.getElementById(id)?.value || "").trim();

    globalMapState.filters.species = readValue("global-filter-species");
    globalMapState.filters.kraj = readValue("global-filter-kraj");
    globalMapState.filters.okres = readValue("global-filter-okres");
    globalMapState.filters.obec = readValue("global-filter-obec");
    globalMapState.filters.sort = readValue("global-filter-sort") || "published_desc";
}

function resetGlobalMapFilters() {
    globalMapState.filters = {
        species: "",
        kraj: "",
        okres: "",
        obec: "",
        sort: "published_desc"
    };
    syncGlobalMapFilterInputs();
    syncGlobalMapQueryString();
    return loadGlobalMapBatch({ reset: true });
}

document.addEventListener("DOMContentLoaded", async () => {
    if (!ensureGlobalMap()) return;

    readGlobalMapFiltersFromQuery();
    syncGlobalMapFilterInputs();

    const loadMoreButton = document.getElementById("global-map-load-more-btn");
    if (loadMoreButton) {
        loadMoreButton.addEventListener("click", () => {
            loadGlobalMapBatch();
        });
    }

    const filterForm = document.getElementById("global-map-filter-form");
    if (filterForm) {
        filterForm.addEventListener("submit", async (event) => {
            event.preventDefault();
            readGlobalMapFiltersFromForm();
            syncGlobalMapQueryString();
            await loadGlobalMapBatch({ reset: true });
        });
    }

    const resetButton = document.getElementById("global-map-filter-reset");
    if (resetButton) {
        resetButton.addEventListener("click", async () => {
            await resetGlobalMapFilters();
        });
    }

    await loadGlobalMapBatch({ reset: true });
});
