const PROFILE_MAP_PAGE_SIZE = 60;
const profileMapState = {
    initialized: false,
    source: "own",
    map: null,
    markerLayer: null,
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
            note: "Mapa slučuje blízké body do animovaných clusterů. Kliknutím cluster rozbalíte a z popupu otevřete fotku.",
            empty: "Zatím jste si za houbičky neodemkli žádné souřadnice."
        };
    }

    return {
        title: "Kde jsem hledal(a)",
        note: "Vaše vlastní fotografie s polohou. Blízké body se seskupují a po přiblížení se plynule rozpadnou na jednotlivé značky.",
        empty: "Zatím nemáte žádné fotografie s uloženou polohou."
    };
}

function profileMapNodes() {
    return {
        shell: document.getElementById("profile-activity-map-shell"),
        map: document.getElementById("profile-activity-map"),
        empty: document.getElementById("profile-activity-empty"),
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
        profileMapState.markerLayer = L.featureGroup().addTo(profileMapState.map);
    }

    return profileMapState.map;
}

function currentProfileDataset() {
    return profileMapState.datasets[profileMapState.source];
}

function currentProfileMapItems() {
    return currentProfileDataset().items.filter((capture) => captureHasCoordinates(capture));
}

function profileCaptureImageURL(capture, variant = "original") {
    if (!capture) {
        return "";
    }

    if (capture.public_url) {
        return buildCaptureImageURL(capture, variant);
    }

    const me = window.appMe || null;
    if (me && Number(me.id) === Number(capture.user_id || capture.author_user_id)) {
        return buildCaptureImageURL(capture, variant);
    }

    return "";
}

function openProfileMapLightbox(captureID) {
    const capturesToOpen = currentProfileDataset().items.filter((capture) => profileCaptureImageURL(capture));
    const startIndex = capturesToOpen.findIndex((capture) => capture.id === captureID);
    if (startIndex === -1) {
        return;
    }

    window.lightboxImages = capturesToOpen.map((capture) => profileCaptureImageURL(capture, "original"));
    window.lightboxCaptureData = capturesToOpen;
    window.lightboxMapData = capturesToOpen.map((capture) => buildCaptureMapData(capture));
    window.currentLightboxIndex = startIndex;

    if (typeof openLightbox === "function") {
        openLightbox();
    }
}

function buildProfilePopupHtml(capture) {
    const imageURL = profileCaptureImageURL(capture, "popup");
    const previewHtml = imageURL
        ? `<img src="${escapeHtml(imageURL)}" alt="${escapeHtml(capture.author_name || "Fotografie")}" loading="lazy">`
        : '<div class="map-popup-placeholder">Bez náhledu</div>';
    const accessLine = capture.coordinates_free
        ? '<p><strong>Souřadnice zdarma</strong></p>'
        : '<p>Soukromý bod na mapě</p>';

    return `
        <div class="map-popup-content">
            ${previewHtml}
            <h4>${escapeHtml(capture.author_name || "Neznámý houbař")}</h4>
            <p>${escapeHtml(formatDateTime(capture.unlocked_at || capture.captured_at))}</p>
            ${accessLine}
            ${imageURL
                ? `<button type="button" class="btn btn-secondary map-popup-action profile-map-open-btn" data-capture-id="${escapeHtml(capture.id)}">Otevřít ve fotkách</button>`
                : ""}
        </div>
    `;
}

function buildProfileMarker(capture) {
    const marker = L.marker([Number(capture.latitude), Number(capture.longitude)]);
    marker.bindPopup(buildProfilePopupHtml(capture));
    marker.on("popupopen", () => {
        const popupNode = marker.getPopup()?.getElement();
        const openButton = popupNode?.querySelector(".profile-map-open-btn");
        if (openButton) {
            openButton.onclick = () => openProfileMapLightbox(capture.id);
        }
    });
    return marker;
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

    if (!items.length) {
        nodes.empty.hidden = false;
        nodes.empty.textContent = labels.empty;
        if (profileMapState.markerLayer && typeof profileMapState.markerLayer.clearLayers === "function") {
            profileMapState.markerLayer.clearLayers();
        }
        return;
    }

    nodes.empty.hidden = true;

    const markers = items.map((capture) => buildProfileMarker(capture));
    if (window.HZDMapClusters) {
        profileMapState.markerLayer = window.HZDMapClusters.replaceLayer(
            map,
            profileMapState.markerLayer,
            markers,
            {
                clusterOptions: {
                    maxClusterRadius: 54,
                    spiderfyDistanceMultiplier: 1.22
                }
            }
        );
        window.HZDMapClusters.fitLayer(map, profileMapState.markerLayer, { padding: [30, 30], maxZoom: 15 });
    } else {
        if (profileMapState.markerLayer && map.hasLayer(profileMapState.markerLayer)) {
            map.removeLayer(profileMapState.markerLayer);
        }
        profileMapState.markerLayer = L.featureGroup(markers).addTo(map);
        const bounds = profileMapState.markerLayer.getBounds();
        if (bounds.isValid()) {
            map.fitBounds(bounds, { padding: [30, 30], maxZoom: 15 });
        }
    }

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
