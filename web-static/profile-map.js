const profileMapState = {
    initialized: false,
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

function profileMapConfig(source) {
    if (source === "viewed") {
        return {
            title: "Prohlédnuté za houbičky",
            buttonId: "profile-activity-viewed-btn",
            loadingLabel: "Prohlédnuté za houbičky · načítám...",
            emptyLabel: "Prohlédnuté za houbičky · 0",
            buildLabel: (count) => `Prohlédnuté za houbičky · ${count}`,
            load: loadViewedProfileActivityPage,
            openLightbox: openViewedProfileMapLightbox
        };
    }

    return {
        title: "Kde jsem hledal(a)",
        buttonId: "profile-activity-own-btn",
        loadingLabel: "Kde jsem hledal(a) · načítám...",
        emptyLabel: "Kde jsem hledal(a) · 0",
        buildLabel: (count) => `Kde jsem hledal(a) · ${count}`,
        load: loadOwnProfileActivityPage,
        openLightbox: openOwnProfileMapLightbox
    };
}

function profileMapButton(source) {
    return document.getElementById(profileMapConfig(source).buttonId);
}

function datasetItemsWithCoordinates(source) {
    const dataset = profileMapState.datasets[source];
    return (dataset?.items || []).filter((capture) => captureHasCoordinates(capture));
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

function openOwnProfileMapLightbox(captureID) {
    const capturesToOpen = profileMapState.datasets.own.items.filter((capture) => profileCaptureImageURL(capture));
    const startIndex = capturesToOpen.findIndex((capture) => capture.id === captureID);
    if (startIndex === -1 || !window.HZDLightbox) {
        return;
    }

    window.HZDLightbox.openCollection(capturesToOpen, startIndex, {
        imageBuilder: (capture) => profileCaptureImageURL(capture, "lightbox"),
        mode: "ownProfileMap",
        onCaptureUpdated: (updatedCapture) => {
            const existing = profileMapState.datasets.own.items.find((capture) => capture.id === updatedCapture.id);
            if (existing) {
                Object.assign(existing, updatedCapture);
            }
            syncProfileMapButtons();
        }
    });
}

function openViewedProfileMapLightbox(captureID) {
    const capturesToOpen = profileMapState.datasets.viewed.items.filter((capture) => profileCaptureImageURL(capture));
    const startIndex = capturesToOpen.findIndex((capture) => capture.id === captureID);
    if (startIndex === -1 || !window.HZDLightbox) {
        return;
    }

    window.HZDLightbox.openCollection(capturesToOpen, startIndex, {
        imageBuilder: (capture) => profileCaptureImageURL(capture, "lightbox")
    });
}

function syncProfileMapButtons() {
    ["own", "viewed"].forEach((source) => {
        const dataset = profileMapState.datasets[source];
        const config = profileMapConfig(source);
        const button = profileMapButton(source);
        if (!button) {
            return;
        }

        if (dataset.loading) {
            button.textContent = config.loadingLabel;
            button.disabled = true;
            return;
        }

        const count = datasetItemsWithCoordinates(source).length;
        button.textContent = config.buildLabel(count);
        button.disabled = count === 0;
    });
}

async function openProfileDatasetMap(source) {
    const dataset = profileMapState.datasets[source];
    const config = profileMapConfig(source);
    if (!dataset) {
        return false;
    }

    if (!dataset.loaded && !dataset.loading) {
        await config.load();
    }

    const items = datasetItemsWithCoordinates(source);
    if (!items.length || !window.HZDMapUI?.openViewer) {
        syncProfileMapButtons();
        return false;
    }

    return window.HZDMapUI.openViewer(items, null, {
        title: config.title,
        note: `${items.length} bodů na mapě.`,
        onCaptureActivate: (capture) => {
            config.openLightbox(capture.id);
        }
    });
}

async function loadOwnProfileActivityPage() {
    const dataset = profileMapState.datasets.own;
    if (dataset.loading || dataset.loaded) {
        return;
    }

    dataset.loading = true;
    syncProfileMapButtons();

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
        syncProfileMapButtons();
    }
}

async function loadViewedProfileActivityPage() {
    const dataset = profileMapState.datasets.viewed;
    if (dataset.loading || dataset.loaded) {
        return;
    }

    dataset.loading = true;
    syncProfileMapButtons();

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
        syncProfileMapButtons();
    }
}

async function initProfileActivityMap() {
    if (document.body.dataset.page !== "me" || profileMapState.initialized) {
        return;
    }

    const ownButton = profileMapButton("own");
    const viewedButton = profileMapButton("viewed");
    if (!ownButton || !viewedButton) {
        return;
    }

    profileMapState.initialized = true;

    ownButton.addEventListener("click", () => {
        openProfileDatasetMap("own");
    });
    viewedButton.addEventListener("click", () => {
        openProfileDatasetMap("viewed");
    });

    syncProfileMapButtons();
    await Promise.all([
        loadOwnProfileActivityPage(),
        loadViewedProfileActivityPage()
    ]);
    syncProfileMapButtons();
}

window.initProfileActivityMap = initProfileActivityMap;
