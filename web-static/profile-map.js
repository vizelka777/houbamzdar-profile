const profileMapState = {
    initialized: false,
    source: "own",
    map: null,
    markerLayer: null,
    datasets: {
        own: {
            items: [],
            loading: false,
            loaded: false
        },
        viewed: {
            items: [],
            loading: false,
            loaded: false
        }
    }
};

function profileMapLabels(source) {
    if (source === "viewed") {
        return {
            title: "Prohlédnuté za houbičky",
            note: "Mapa slučuje blízké body do animovaných clusterů. Kliknutím cluster rozbalíte a kliknutím na miniaturu otevřete fotku.",
            empty: "Zatím jste si za houbičky neodemkli žádné souřadnice."
        };
    }

    return {
        title: "Kde jsem hledal(a)",
        note: "Vaše vlastní fotografie s polohou. Blízké body se seskupují a po přiblížení se plynule rozpadnou na jednotlivé miniatury.",
        empty: "Zatím nemáte žádné fotografie s uloženou polohou."
    };
}

function profileMapNodes() {
    return {
        shell: document.getElementById("profile-activity-map-shell"),
        map: document.getElementById("profile-activity-map"),
        empty: document.getElementById("profile-activity-empty"),
        summary: document.getElementById("profile-activity-summary"),
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
    if (startIndex === -1 || !window.HZDLightbox) {
        return;
    }

    window.HZDLightbox.openCollection(capturesToOpen, startIndex, {
        imageBuilder: (capture) => profileCaptureImageURL(capture, "lightbox")
    });
}

function buildProfilePopupHtml(capture) {
    const imageURL = profileCaptureImageURL(capture, "popup");
    return window.HZDMapUI.buildPopupHtml({
        authorName: capture.author_name || "Neznámý houbař",
        previewUrl: capture.public_url ? "" : imageURL,
        previewHtml: capture.public_url ? buildCapturePopupPreviewHtml(capture, capture.author_name || "Fotografie") : "",
        altText: capture.author_name || "Fotografie",
        dateValue: capture.unlocked_at || capture.captured_at,
        metaLines: [capture.coordinates_free ? "Souřadnice zdarma" : "Soukromý bod na mapě"],
        actionHtml: imageURL
            ? `<button type="button" class="btn btn-secondary map-popup-action profile-map-open-btn" data-capture-id="${escapeHtml(capture.id)}">Otevřít ve fotkách</button>`
            : ""
    });
}

function buildProfileMarker(capture) {
    const markerTitle = buildCaptureSpeciesLabel(capture) || capture.author_name || "Otevřít fotografii";
    const markerTooltip = window.HZDMapUI?.buildMarkerTooltipHtml
        ? window.HZDMapUI.buildMarkerTooltipHtml({
            title: markerTitle,
            metaLines: [capture.coordinates_free ? "Souřadnice zdarma" : "Soukromý bod na mapě"]
        })
        : "";

    if (window.HZDMapUI?.createCaptureMarker) {
        return window.HZDMapUI.createCaptureMarker(capture, {
            title: markerTitle,
            tooltipHtml: markerTooltip,
            onActivate: () => {
                openProfileMapLightbox(capture.id);
            }
        });
    }

    const marker = L.marker([Number(capture.latitude), Number(capture.longitude)]);
    marker.bindPopup(buildProfilePopupHtml(capture));
    if (window.HZDMapUI) {
        window.HZDMapUI.bindPopupAction(marker, ".profile-map-open-btn", () => {
            openProfileMapLightbox(capture.id);
        });
    }
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
    if (dataset.loading || dataset.loaded) {
        return;
    }

    dataset.loading = true;
    renderProfileActivitySummary();

    try {
        const result = await apiGet("/api/me/map-captures");
        if (!result || !result.ok) {
            throw new Error("Nepodařilo se načíst soukromou mapu.");
        }

        dataset.items = Array.isArray(result.captures) ? result.captures : [];
        dataset.loaded = true;
    } catch (error) {
        console.error("Failed to load own profile activity map", error);
    } finally {
        dataset.loading = false;
        renderProfileActivityMap();
    }
}

async function loadViewedProfileActivityPage() {
    const dataset = profileMapState.datasets.viewed;
    if (dataset.loading || dataset.loaded) {
        return;
    }

    dataset.loading = true;
    renderProfileActivitySummary();

    try {
        const result = await apiGet("/api/me/viewed-map-captures");
        if (!result || !result.ok) {
            throw new Error("Nepodařilo se načíst odemčené souřadnice.");
        }

        dataset.items = Array.isArray(result.captures) ? result.captures : [];
        dataset.loaded = true;
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

    if (!dataset.loaded && !dataset.loading) {
        if (source === "viewed") {
            await loadViewedProfileActivityPage();
            return;
        }
        await loadOwnProfileActivityPage();
        return;
    }

    renderProfileActivityMap();
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

    await Promise.all([
        loadOwnProfileActivityPage(),
        loadViewedProfileActivityPage()
    ]);
    renderProfileActivityMap();
}

window.initProfileActivityMap = initProfileActivityMap;
