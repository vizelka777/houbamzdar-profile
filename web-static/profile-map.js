const PROFILE_MAP_PAGE_SIZE = 60;
const profileMapState = {
    initialized: false,
    source: "own",
    map: null,
    markerLayer: null,
    selectedCaptureID: "",
    neighborhood: [],
    datasets: {
        own: {
            items: [],
            page: 0,
            total: 0,
            totalPages: 0,
            hasMore: true,
            loading: false
        },
        viewed: {
            items: [],
            offset: 0,
            total: 0,
            hasMore: true,
            loading: false
        }
    }
};

function profileMapLabels(source) {
    if (source === "viewed") {
        return {
            title: "Prohlédnuté za houbičky",
            note: "Odemčené souřadnice zůstávají ve vaší soukromé mapě. Kliknutím na značku mapu rozšíříte a zobrazíte sousední fotografie.",
            empty: "Zatím jste si za houbičky neodemkli žádné souřadnice."
        };
    }

    return {
        title: "Kde jsem hledal(a)",
        note: "Vaše vlastní fotografie s polohou. Kliknutím na značku mapu rozšíříte a zobrazíte okolní snímky.",
        empty: "Zatím nemáte žádné fotografie s uloženou polohou."
    };
}

function profileMapNodes() {
    return {
        shell: document.getElementById("profile-activity-map-shell"),
        map: document.getElementById("profile-activity-map"),
        empty: document.getElementById("profile-activity-empty"),
        strip: document.getElementById("profile-activity-strip"),
        summary: document.getElementById("profile-activity-summary"),
        loadMore: document.getElementById("profile-activity-load-more-btn"),
        title: document.getElementById("profile-activity-title"),
        note: document.getElementById("profile-activity-note")
    };
}

function ensureProfileMap() {
    const nodes = profileMapNodes();
    if (!nodes.map || typeof L === "undefined") {
        return null;
    }

    if (!profileMapState.map) {
        profileMapState.map = L.map("profile-activity-map").setView([49.8, 15.5], 7);
        L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
            attribution: "&copy; OpenStreetMap"
        }).addTo(profileMapState.map);
        profileMapState.markerLayer = L.layerGroup().addTo(profileMapState.map);
        profileMapState.map.on("click", () => {
            profileMapState.selectedCaptureID = "";
            profileMapState.neighborhood = [];
            renderProfileActivityMap();
        });
    }

    return profileMapState.map;
}

function toMetersOffset(lat, dx, dy) {
    const latOffset = dy / 111111;
    const lonOffset = dx / (111111 * Math.max(Math.cos((lat * Math.PI) / 180), 0.2));
    return { latOffset, lonOffset };
}

function profileDistanceMeters(a, b) {
    const toRadians = (value) => (value * Math.PI) / 180;
    const earthRadius = 6371000;
    const dLat = toRadians(Number(b.latitude) - Number(a.latitude));
    const dLon = toRadians(Number(b.longitude) - Number(a.longitude));
    const lat1 = toRadians(Number(a.latitude));
    const lat2 = toRadians(Number(b.latitude));

    const x = Math.sin(dLat / 2) ** 2 + Math.cos(lat1) * Math.cos(lat2) * Math.sin(dLon / 2) ** 2;
    return 2 * earthRadius * Math.atan2(Math.sqrt(x), Math.sqrt(1 - x));
}

function currentProfileDataset() {
    return profileMapState.datasets[profileMapState.source];
}

function currentProfileMapItems() {
    return currentProfileDataset().items.filter((capture) => captureHasCoordinates(capture));
}

function buildMapNeighborhood(selectedCapture, items) {
    if (!selectedCapture) {
        return [];
    }

    const neighbors = items
        .filter((capture) => capture.id !== selectedCapture.id)
        .map((capture) => ({
            capture,
            distance: profileDistanceMeters(selectedCapture, capture)
        }))
        .sort((left, right) => left.distance - right.distance)
        .filter((entry) => entry.distance <= 350)
        .slice(0, 6)
        .map((entry) => entry.capture);

    return [selectedCapture].concat(neighbors);
}

function profileCaptureImageURL(capture) {
    if (!capture) {
        return "";
    }
    if (capture.public_url) {
        return buildCaptureImageURL(capture);
    }
    const me = window.appMe || null;
    if (me && Number(me.id) === Number(capture.user_id || capture.author_user_id)) {
        return buildCaptureImageURL(capture);
    }
    return "";
}

function buildSpreadMarkers(neighborhood) {
    if (!neighborhood.length) {
        return [];
    }

    const [selectedCapture, ...rest] = neighborhood;
    const baseLat = Number(selectedCapture.latitude);
    const baseLon = Number(selectedCapture.longitude);
    const markers = [{
        capture: selectedCapture,
        lat: baseLat,
        lon: baseLon,
        selected: true
    }];

    rest.forEach((capture, index) => {
        const angle = (index / Math.max(rest.length, 1)) * Math.PI * 2;
        const radius = 60 + (index % 3) * 28;
        const offset = toMetersOffset(baseLat, Math.cos(angle) * radius, Math.sin(angle) * radius);
        markers.push({
            capture,
            lat: baseLat + offset.latOffset,
            lon: baseLon + offset.lonOffset,
            selected: false
        });
    });

    return markers;
}

function markerThumbnailHtml(capture, selected) {
    const imageURL = profileCaptureImageURL(capture);
    const label = capture.author_name || "Houbar";
    const badge = capture.coordinates_free ? '<span class="profile-map-thumb-badge">Zdarma</span>' : "";

    return `
        <div class="profile-map-thumb ${selected ? "is-selected" : ""}">
            ${imageURL
                ? `<img src="${escapeHtml(imageURL)}" alt="${escapeHtml(label)}" loading="lazy">`
                : `<span class="profile-map-thumb-placeholder">${escapeHtml(label.slice(0, 1).toUpperCase())}</span>`}
            ${badge}
        </div>
    `;
}

function buildProfileMarker(capture, lat, lon, selected) {
    const marker = L.marker([lat, lon], {
        icon: L.divIcon({
            className: "profile-map-thumb-icon",
            html: markerThumbnailHtml(capture, selected),
            iconSize: selected ? [78, 78] : [64, 64],
            iconAnchor: selected ? [39, 72] : [32, 58]
        })
    });

    marker.on("click", () => {
        profileMapState.selectedCaptureID = capture.id;
        renderProfileActivityMap();
    });

    return marker;
}

function buildStandardProfileMarker(capture) {
    const marker = L.marker([Number(capture.latitude), Number(capture.longitude)]);
    marker.on("click", () => {
        profileMapState.selectedCaptureID = capture.id;
        renderProfileActivityMap();
    });
    marker.bindPopup(`
        <div class="map-popup-content">
            ${profileCaptureImageURL(capture)
                ? `<img src="${escapeHtml(profileCaptureImageURL(capture))}" alt="${escapeHtml(capture.author_name || "Fotografie")}" loading="lazy">`
                : `<div class="map-popup-placeholder">Bez náhledu</div>`}
            <h4>${escapeHtml(capture.author_name || "Neznámý houbař")}</h4>
            <p>${escapeHtml(formatDateTime(capture.unlocked_at || capture.captured_at))}</p>
        </div>
    `);
    return marker;
}

function renderProfileActivityStrip() {
    const nodes = profileMapNodes();
    if (!nodes.strip) {
        return;
    }

    if (!profileMapState.neighborhood.length) {
        nodes.strip.innerHTML = "";
        return;
    }

    const captures = profileMapState.neighborhood;
    nodes.strip.innerHTML = captures.map((capture) => `
        <button type="button" class="profile-activity-strip-item ${capture.id === profileMapState.selectedCaptureID ? "is-active" : ""}" data-capture-id="${escapeHtml(capture.id)}" ${profileCaptureImageURL(capture) ? "" : "disabled"}>
            ${profileCaptureImageURL(capture)
                ? `<img src="${escapeHtml(profileCaptureImageURL(capture))}" alt="${escapeHtml(capture.author_name || "Fotografie")}" loading="lazy">`
                : `<div class="profile-map-thumb-placeholder">Bez náhledu</div>`}
            <span>${escapeHtml(capture.author_name || "Neznámý houbař")}</span>
        </button>
    `).join("");

    nodes.strip.querySelectorAll(".profile-activity-strip-item").forEach((button) => {
        button.addEventListener("click", () => {
            const captureID = button.dataset.captureId;
            const capturesToOpen = profileMapState.neighborhood.filter((capture) => profileCaptureImageURL(capture));
            const startIndex = capturesToOpen.findIndex((capture) => capture.id === captureID);
            if (!capturesToOpen.length) {
                return;
            }

            window.lightboxImages = capturesToOpen.map((capture) => profileCaptureImageURL(capture));
            window.lightboxCaptureData = capturesToOpen;
            window.lightboxMapData = capturesToOpen.map((capture) => buildCaptureMapData(capture));
            window.currentLightboxIndex = Math.max(startIndex, 0);
            if (typeof openLightbox === "function") {
                openLightbox();
            }
        });
    });
}

function renderProfileActivitySummary() {
    const nodes = profileMapNodes();
    const dataset = currentProfileDataset();
    const withCoordinates = dataset.items.filter((capture) => captureHasCoordinates(capture)).length;
    const labels = profileMapLabels(profileMapState.source);

    if (nodes.title) {
        nodes.title.textContent = labels.title;
    }
    if (nodes.note) {
        nodes.note.textContent = labels.note;
    }
    if (nodes.summary) {
        nodes.summary.textContent = `Na mapě je ${withCoordinates} z ${dataset.items.length} načtených fotografií.`;
    }
    if (nodes.loadMore) {
        nodes.loadMore.style.display = dataset.hasMore ? "inline-flex" : "none";
        nodes.loadMore.disabled = dataset.loading;
    }

    document.querySelectorAll(".profile-activity-toggle").forEach((button) => {
        button.classList.toggle("is-active", button.dataset.source === profileMapState.source);
    });
}

function renderProfileActivityMap() {
    const nodes = profileMapNodes();
    const map = ensureProfileMap();
    const items = currentProfileMapItems();
    const labels = profileMapLabels(profileMapState.source);

    renderProfileActivitySummary();

    if (!map || !nodes.shell || !nodes.empty) {
        return;
    }

    if (profileMapState.markerLayer) {
        profileMapState.markerLayer.clearLayers();
    }

    if (!items.length) {
        nodes.empty.hidden = false;
        nodes.empty.textContent = labels.empty;
        nodes.shell.classList.remove("is-expanded");
        profileMapState.neighborhood = [];
        renderProfileActivityStrip();
        return;
    }

    nodes.empty.hidden = true;

    const selectedCapture = items.find((capture) => capture.id === profileMapState.selectedCaptureID) || null;
    const bounds = L.latLngBounds();

    if (selectedCapture) {
        const neighborhood = buildMapNeighborhood(selectedCapture, items);
        const spreadMarkers = buildSpreadMarkers(neighborhood);
        profileMapState.neighborhood = neighborhood;
        nodes.shell.classList.add("is-expanded");

        items.forEach((capture) => {
            if (neighborhood.some((item) => item.id === capture.id)) {
                return;
            }
            const marker = buildStandardProfileMarker(capture);
            marker.addTo(profileMapState.markerLayer);
            bounds.extend([Number(capture.latitude), Number(capture.longitude)]);
        });

        spreadMarkers.forEach((entry) => {
            const marker = buildProfileMarker(entry.capture, entry.lat, entry.lon, entry.selected);
            marker.addTo(profileMapState.markerLayer);
            bounds.extend([entry.lat, entry.lon]);
        });

        if (bounds.isValid()) {
            map.fitBounds(bounds, { padding: [40, 40], maxZoom: 15 });
        } else {
            map.setView([Number(selectedCapture.latitude), Number(selectedCapture.longitude)], 15);
        }
    } else {
        nodes.shell.classList.remove("is-expanded");
        profileMapState.neighborhood = [];

        items.forEach((capture) => {
            const marker = buildStandardProfileMarker(capture);
            marker.addTo(profileMapState.markerLayer);
            bounds.extend([Number(capture.latitude), Number(capture.longitude)]);
        });

        if (bounds.isValid()) {
            map.fitBounds(bounds, { padding: [32, 32] });
        }
    }

    renderProfileActivityStrip();
    window.setTimeout(() => map.invalidateSize(), 0);
}

async function loadOwnProfileActivityPage() {
    const dataset = profileMapState.datasets.own;
    if (dataset.loading || !dataset.hasMore) {
        return;
    }

    dataset.loading = true;
    renderProfileActivitySummary();

    try {
        const nextPage = dataset.page + 1;
        const result = await apiGet(`/api/captures?page=${nextPage}&page_size=${PROFILE_MAP_PAGE_SIZE}`);
        if (!result || !result.ok) {
            throw new Error("Nepodařilo se načíst soukromou mapu.");
        }

        dataset.page = result.page || nextPage;
        dataset.total = result.total || dataset.total;
        dataset.totalPages = result.total_pages || dataset.totalPages;
        dataset.items = dataset.items.concat(result.captures || []);
        dataset.hasMore = dataset.page < dataset.totalPages;
    } catch (error) {
        console.error("Failed to load own profile activity map", error);
    } finally {
        dataset.loading = false;
        renderProfileActivityMap();
    }
}

async function loadViewedProfileActivityPage() {
    const dataset = profileMapState.datasets.viewed;
    if (dataset.loading || !dataset.hasMore) {
        return;
    }

    dataset.loading = true;
    renderProfileActivitySummary();

    try {
        const result = await apiGet(`/api/me/viewed-captures?limit=${PROFILE_MAP_PAGE_SIZE}&offset=${dataset.offset}`);
        if (!result || !result.ok) {
            throw new Error("Nepodařilo se načíst odemčené souřadnice.");
        }

        const captures = result.captures || [];
        dataset.items = dataset.items.concat(captures);
        dataset.offset += captures.length;
        dataset.total = result.total || dataset.total;
        dataset.hasMore = Boolean(result.has_more);
    } catch (error) {
        console.error("Failed to load viewed activity map", error);
    } finally {
        dataset.loading = false;
        renderProfileActivityMap();
    }
}

async function switchProfileActivitySource(source) {
    if (!profileMapState.datasets[source]) {
        return;
    }

    profileMapState.source = source;
    profileMapState.selectedCaptureID = "";
    profileMapState.neighborhood = [];

    const dataset = currentProfileDataset();
    if (!dataset.items.length && dataset.hasMore) {
        if (source === "viewed") {
            await loadViewedProfileActivityPage();
            return;
        }
        await loadOwnProfileActivityPage();
        return;
    }

    renderProfileActivityMap();
}

async function loadMoreProfileActivity() {
    if (profileMapState.source === "viewed") {
        await loadViewedProfileActivityPage();
        return;
    }
    await loadOwnProfileActivityPage();
}

async function initProfileActivityMap() {
    if (document.body.dataset.page !== "me") {
        return;
    }

    const nodes = profileMapNodes();
    if (!nodes.map || profileMapState.initialized) {
        return;
    }

    profileMapState.initialized = true;

    document.querySelectorAll(".profile-activity-toggle").forEach((button) => {
        button.addEventListener("click", () => {
            switchProfileActivitySource(button.dataset.source || "own");
        });
    });

    if (nodes.loadMore) {
        nodes.loadMore.addEventListener("click", () => {
            loadMoreProfileActivity();
        });
    }

    await Promise.all([
        loadOwnProfileActivityPage(),
        loadViewedProfileActivityPage()
    ]);
    renderProfileActivityMap();
}

window.initProfileActivityMap = initProfileActivityMap;
