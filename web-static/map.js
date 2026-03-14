const globalMapState = {
    pageSize: 120,
    offset: 0,
    hasMore: true,
    loaded: 0,
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

function updateGlobalMapSummary() {
    const summary = document.getElementById("global-map-summary");
    const loadMoreButton = document.getElementById("global-map-load-more-btn");
    if (summary) {
        summary.textContent = `Načteno ${globalMapState.loaded} veřejných fotografií s polohou.`;
    }
    if (loadMoreButton) {
        loadMoreButton.style.display = globalMapState.hasMore ? "inline-flex" : "none";
    }
}

async function loadGlobalMapBatch() {
    const map = ensureGlobalMap();
    if (!map || !globalMapState.hasMore) {
        return;
    }

    const result = await apiGet(`/api/public/captures?limit=${globalMapState.pageSize}&offset=${globalMapState.offset}`);
    if (!result || !result.ok || !Array.isArray(result.captures)) {
        return;
    }

    const captures = result.captures.filter((capture) => captureHasCoordinates(capture));
    const markers = captures.map((capture) => {
        const lat = Number(capture.latitude);
        const lon = Number(capture.longitude);
        if (Number.isNaN(lat) || Number.isNaN(lon)) {
            return null;
        }

        globalMapState.bounds.extend([lat, lon]);
        const author = escapeHtml(capture.author_name || "Neznámý houbař");
        const authorUrl = escapeHtml(buildPublicProfileURL(capture.author_user_id));
        const date = escapeHtml(formatDateTime(capture.captured_at));
        const imageHtml = capture.public_url
            ? `<a href="${escapeHtml(buildCaptureImageURL(capture))}" target="_blank" rel="noreferrer"><img src="${escapeHtml(buildCaptureImageURL(capture))}" alt="${author}" loading="lazy"></a>`
            : '<div class="map-popup-placeholder">Bez veřejného náhledu</div>';

        const marker = L.marker([lat, lon]);
        marker.bindPopup(`
            <div class="map-popup-content">
                ${imageHtml}
                <h4><a href="${authorUrl}">${author}</a></h4>
                <p>${date}</p>
            </div>
        `);
        return marker;
    }).filter(Boolean);

    if (window.HZDMapClusters) {
        const mergedMarkers = [];
        if (globalMapState.markerLayer && typeof globalMapState.markerLayer.eachLayer === "function") {
            globalMapState.markerLayer.eachLayer((layer) => mergedMarkers.push(layer));
        }
        markers.forEach((marker) => mergedMarkers.push(marker));
        globalMapState.markerLayer = window.HZDMapClusters.replaceLayer(
            map,
            globalMapState.markerLayer,
            mergedMarkers,
            {
                clusterOptions: {
                    maxClusterRadius: 58,
                    spiderfyDistanceMultiplier: 1.24
                }
            }
        );
    } else {
        markers.forEach((marker) => marker.addTo(globalMapState.markerLayer));
    }

    globalMapState.loaded += captures.length;
    globalMapState.offset += result.captures.length;
    globalMapState.hasMore = result.captures.length === globalMapState.pageSize;

    if (window.HZDMapClusters && globalMapState.markerLayer) {
        window.HZDMapClusters.fitLayer(map, globalMapState.markerLayer, { padding: [30, 30], maxZoom: 15 });
    } else if (globalMapState.bounds.isValid()) {
        map.fitBounds(globalMapState.bounds, { padding: [30, 30], maxZoom: 15 });
    }
    updateGlobalMapSummary();
}

document.addEventListener("DOMContentLoaded", async () => {
    if (!ensureGlobalMap()) return;

    const loadMoreButton = document.getElementById("global-map-load-more-btn");
    if (loadMoreButton) {
        loadMoreButton.addEventListener("click", () => {
            loadGlobalMapBatch();
        });
    }

    await loadGlobalMapBatch();
});
