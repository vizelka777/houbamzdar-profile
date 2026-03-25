const myMapState = {
    currentSource: "own",
    datasets: {
        own: { items: [], loading: false, loaded: false },
        viewed: { items: [], loading: false, loaded: false }
    }
};

function myMapImageURL(capture, variant = "original") {
    if (!capture) return "";
    if (capture.public_url) return buildCaptureImageURL(capture, variant);
    const me = window.appMe || null;
    if (me && Number(me.id) === Number(capture.user_id || capture.author_user_id)) {
        return buildCaptureImageURL(capture, variant);
    }
    return "";
}

function updateMyMapButtons() {
    const ownBtn = document.getElementById("profile-activity-own-btn");
    const viewedBtn = document.getElementById("profile-activity-viewed-btn");
    
    if (ownBtn) {
        const ds = myMapState.datasets.own;
        const count = ds.items.filter(c => captureHasCoordinates(c)).length;
        if (ds.loading) {
            ownBtn.textContent = "Kde jsem hledal(a) · načítám...";
        } else {
            ownBtn.textContent = `Kde jsem hledal(a) · ${count}`;
        }
        if (myMapState.currentSource === "own") {
            ownBtn.className = "btn btn-primary profile-activity-launch-btn";
        } else {
            ownBtn.className = "btn btn-secondary profile-activity-launch-btn";
        }
    }
    
    if (viewedBtn) {
        const ds = myMapState.datasets.viewed;
        const count = ds.items.filter(c => captureHasCoordinates(c)).length;
        if (ds.loading) {
            viewedBtn.textContent = "Prohlédnuté za houbičky · načítám...";
        } else {
            viewedBtn.textContent = `Prohlédnuté za houbičky · ${count}`;
        }
        if (myMapState.currentSource === "viewed") {
            viewedBtn.className = "btn btn-primary profile-activity-launch-btn";
        } else {
            viewedBtn.className = "btn btn-secondary profile-activity-launch-btn";
        }
    }
}

function renderMyMap() {
    if (!window.captureMapViewerInstance) return;
    
    const source = myMapState.currentSource;
    const ds = myMapState.datasets[source];
    const captures = ds.items.filter(c => captureHasCoordinates(c));
    
    const noteNode = document.getElementById("capture-map-viewer-note");
    if (noteNode) {
        if (ds.loading) {
            noteNode.textContent = "Načítám mapu...";
        } else {
            noteNode.textContent = `${captures.length} bodů na mapě.`;
        }
    }
    
    if (ds.loading) return;

    const entries = normalizeMapViewerEntries(captures, null);
    
    // Clear old map
    if (window.captureMapViewerMarkerLayer && window.captureMapViewerInstance.hasLayer(window.captureMapViewerMarkerLayer)) {
        window.captureMapViewerInstance.removeLayer(window.captureMapViewerMarkerLayer);
        window.captureMapViewerMarkerLayer = null;
    }
    
    if (entries.length === 0) return;
    
    renderCaptureMapViewerEntries(window.captureMapViewerInstance, entries, {
        onCaptureActivate: (captureData) => {
            const capturesToOpen = captures.filter(c => myMapImageURL(c));
            const startIndex = capturesToOpen.findIndex(c => c.id === captureData.id);
            if (startIndex !== -1 && window.HZDLightbox) {
                window.HZDLightbox.openCollection(capturesToOpen, startIndex, {
                    imageBuilder: c => myMapImageURL(c, "lightbox"),
                    mode: source === "own" ? "ownProfileMap" : null,
                    onCaptureUpdated: (updatedCapture) => {
                        if (source === "own") {
                            const existing = ds.items.find(c => c.id === updatedCapture.id);
                            if (existing) {
                                Object.assign(existing, updatedCapture);
                            }
                            updateMyMapButtons();
                            renderMyMap();
                        }
                    }
                });
            }
        }
    });
}

async function loadDataset(source) {
    const ds = myMapState.datasets[source];
    if (ds.loading || ds.loaded) return;
    
    ds.loading = true;
    updateMyMapButtons();
    if (myMapState.currentSource === source) renderMyMap();
    
    try {
        const endpoint = source === "own" ? "/api/me/map-captures" : "/api/me/viewed-map-captures";
        const result = await apiGet(endpoint);
        if (!result || !result.ok) {
            throw new Error(`Nepodařilo se načíst mapu (${source}).`);
        }
        ds.items = Array.isArray(result.captures) ? result.captures : [];
        ds.loaded = true;
    } catch (error) {
        console.error("Failed to load map dataset", source, error);
    } finally {
        ds.loading = false;
        updateMyMapButtons();
        if (myMapState.currentSource === source) renderMyMap();
    }
}

async function initMyMapPage() {
    if (document.body.dataset.page !== "my-map") return;

    window.captureMapViewerInstance = L.map("my-map-container", {
        zoomControl: true
    }).setView([49.8, 15.5], 7);

    L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
        attribution: "&copy; OpenStreetMap",
        maxZoom: 19
    }).addTo(window.captureMapViewerInstance);
    
    window.captureMapViewerMarkerLayer = L.layerGroup().addTo(window.captureMapViewerInstance);
    
    const ownBtn = document.getElementById("profile-activity-own-btn");
    const viewedBtn = document.getElementById("profile-activity-viewed-btn");
    
    if (ownBtn) {
        ownBtn.addEventListener("click", () => {
            myMapState.currentSource = "own";
            updateMyMapButtons();
            renderMyMap();
            if (!myMapState.datasets.own.loaded) loadDataset("own");
        });
    }
    
    if (viewedBtn) {
        viewedBtn.addEventListener("click", () => {
            myMapState.currentSource = "viewed";
            updateMyMapButtons();
            renderMyMap();
            if (!myMapState.datasets.viewed.loaded) loadDataset("viewed");
        });
    }
    
    updateMyMapButtons();
    renderMyMap();
    
    // Load "own" dataset by default
    await loadDataset("own");
    // Preload "viewed" in background
    loadDataset("viewed");
}

document.addEventListener("DOMContentLoaded", initMyMapPage);