const state = {
    captures: [],
    page: 1,
    pageSize: 24,
    hasMore: true,
    isLoading: false,
    session: null,
    me: null,
    filters: {
        species: "",
        kraj: "",
        okres: "",
        obec: "",
        sort: "published_desc"
    },
    moderation: {
        models: [],
        defaultModel: "",
        selectedModel: "",
        loading: false,
        loadError: ""
    }
};

function readGalleryFiltersFromQuery() {
    const query = new URLSearchParams(window.location.search);
    state.filters.species = (query.get("species") || "").trim();
    state.filters.kraj = (query.get("kraj") || "").trim();
    state.filters.okres = (query.get("okres") || "").trim();
    state.filters.obec = (query.get("obec") || "").trim();
    state.filters.sort = (query.get("sort") || "published_desc").trim() || "published_desc";
}

function syncGalleryFilterInputs() {
    const setValue = (id, value) => {
        const input = document.getElementById(id);
        if (input) {
            input.value = value;
        }
    };

    setValue("gallery-filter-species", state.filters.species);
    setValue("gallery-filter-kraj", state.filters.kraj);
    setValue("gallery-filter-okres", state.filters.okres);
    setValue("gallery-filter-obec", state.filters.obec);
    setValue("gallery-filter-sort", state.filters.sort);
}

function syncGalleryQueryString() {
    const params = new URLSearchParams();
    if (state.filters.species) params.set("species", state.filters.species);
    if (state.filters.kraj) params.set("kraj", state.filters.kraj);
    if (state.filters.okres) params.set("okres", state.filters.okres);
    if (state.filters.obec) params.set("obec", state.filters.obec);
    if (state.filters.sort && state.filters.sort !== "published_desc") {
        params.set("sort", state.filters.sort);
    }

    const query = params.toString();
    const nextURL = query ? `${window.location.pathname}?${query}` : window.location.pathname;
    window.history.replaceState({}, "", nextURL);
}

function readGalleryFiltersFromForm() {
    const readValue = (id) => (document.getElementById(id)?.value || "").trim();

    state.filters.species = readValue("gallery-filter-species");
    state.filters.kraj = readValue("gallery-filter-kraj");
    state.filters.okres = readValue("gallery-filter-okres");
    state.filters.obec = readValue("gallery-filter-obec");
    state.filters.sort = readValue("gallery-filter-sort") || "published_desc";
}

function buildGalleryQuery() {
    const params = new URLSearchParams();
    params.set("limit", String(state.pageSize));
    params.set("offset", String((state.page - 1) * state.pageSize));
    if (state.filters.species) params.set("species", state.filters.species);
    if (state.filters.kraj) params.set("kraj", state.filters.kraj);
    if (state.filters.okres) params.set("okres", state.filters.okres);
    if (state.filters.obec) params.set("obec", state.filters.obec);
    if (state.filters.sort) params.set("sort", state.filters.sort);
    return params.toString();
}

function updateGallerySummary() {
    const summary = document.getElementById("gallery-summary");
    if (!summary) return;

    if (state.isLoading && state.captures.length === 0) {
        summary.textContent = "Načítám fotografie...";
        return;
    }
    if (state.captures.length === 0) {
        summary.textContent = "Pro tento filtr zatím není žádná veřejná fotografie.";
        return;
    }

    summary.textContent = `Načteno ${state.captures.length} veřejných fotografií pro aktuální filtr.`;
}

function canModeratorRecheck() {
    return Boolean(state.me && state.me.is_moderator);
}

function syncModeratorModelPanel() {
    const panel = document.getElementById("gallery-moderator-panel");
    const select = document.getElementById("gallery-moderator-model");
    const note = document.getElementById("gallery-moderator-note");
    if (!panel || !select || !note) {
        return;
    }

    if (!canModeratorRecheck()) {
        panel.hidden = true;
        return;
    }

    panel.hidden = false;
    select.innerHTML = "";

    if (state.moderation.loading) {
        const option = document.createElement("option");
        option.value = "";
        option.textContent = "Načítám modely...";
        select.appendChild(option);
        select.disabled = true;
        note.textContent = "Načítám dostupné Gemini modely z validátoru.";
        return;
    }

    if (state.moderation.models.length === 0) {
        const option = document.createElement("option");
        option.value = "";
        option.textContent = "Výchozí model backendu";
        select.appendChild(option);
        select.disabled = true;
        note.textContent = state.moderation.loadError
            || "Seznam modelů se nepodařilo načíst. Recheck použije výchozí model backendu.";
        return;
    }

    state.moderation.models.forEach((model) => {
        const option = document.createElement("option");
        option.value = model.code;
        option.textContent = model.label || model.code;
        select.appendChild(option);
    });

    const fallbackModel = state.moderation.defaultModel || state.moderation.models[0].code;
    const selectedModel = state.moderation.selectedModel || fallbackModel;
    select.disabled = false;
    select.value = selectedModel;
    state.moderation.selectedModel = select.value;
    note.textContent = `Dostupné Gemini modely pro moderatorský recheck: ${state.moderation.models.length}. Aktuálně vybraný model: ${select.value}.`;
}

async function loadModeratorModels() {
    if (!canModeratorRecheck()) {
        return;
    }

    state.moderation.loading = true;
    state.moderation.loadError = "";
    syncModeratorModelPanel();

    try {
        const response = await apiGet("/api/moderation/ai-models");
        if (!response || !response.ok) {
            throw new Error("Nepodařilo se načíst seznam modelů.");
        }

        const models = Array.isArray(response.models)
            ? response.models
                .map((item) => ({
                    code: String(item?.code || "").trim(),
                    label: String(item?.label || item?.code || "").trim()
                }))
                .filter((item) => item.code)
            : [];

        state.moderation.models = models;
        state.moderation.defaultModel = String(response.default_model || "").trim();

        const currentModelValid = state.moderation.selectedModel
            && models.some((item) => item.code === state.moderation.selectedModel);
        if (!currentModelValid) {
            state.moderation.selectedModel = state.moderation.defaultModel || models[0]?.code || "";
        }
    } catch (error) {
        console.error("Failed to load moderator AI models", error);
        state.moderation.models = [];
        state.moderation.defaultModel = "";
        state.moderation.selectedModel = "";
        state.moderation.loadError = error.message || "Nepodařilo se načíst seznam modelů.";
    } finally {
        state.moderation.loading = false;
        syncModeratorModelPanel();
    }
}

async function runModeratorRecheck(captureID, button) {
    if (!captureID || !canModeratorRecheck()) {
        return;
    }
    const selectedModel = state.moderation.selectedModel || state.moderation.defaultModel;
    const modelLabel = selectedModel || "výchozí model";
    if (!window.confirm(`Spustit moderatorskou AI kontrolu přes ${modelLabel} a přepsat rozpoznané druhy?`)) {
        return;
    }

    if (button) {
        button.disabled = true;
    }

    try {
        const response = await fetch(`${API_URL}/api/captures/${encodeURIComponent(captureID)}/moderator-recheck`, {
            method: "POST",
            credentials: "include",
            headers: {
                "Content-Type": "application/json"
            },
            body: JSON.stringify(selectedModel ? { model_code: selectedModel } : {})
        });
        const payload = await response.json().catch(() => null);
        if (!response.ok || !payload || !payload.ok) {
            throw new Error(payload?.error || `HTTP ${response.status}`);
        }

        const firstSpecies = Array.isArray(payload.species) && payload.species.length > 0
            ? payload.species[0].latin_name || "aktualizovaný taxon"
            : "aktualizovaný taxon";
        window.alert(`Moderatorská kontrola je hotová. Hlavní taxon: ${firstSpecies}.`);
        await loadGallery({ reset: true });
    } catch (error) {
        console.error("Moderator recheck failed", error);
        window.alert(error.message || "Moderatorská kontrola se nepodařila.");
    } finally {
        if (button) {
            button.disabled = false;
        }
    }
}

function renderGallery(container) {
    if (!container) return;

    if (state.captures.length === 0) {
        container.innerHTML = '<p class="muted-copy" style="grid-column: 1 / -1; text-align: center;">Zatím nejsou sdíleny žádné fotografie pro tento filtr.</p>';
        return;
    }

    window.lightboxImages = state.captures.map((capture) => buildCaptureImageURL(capture, "original"));
    window.lightboxCaptureData = state.captures;
    window.lightboxMapData = state.captures.map((capture) => buildCaptureMapData(capture));

    container.innerHTML = state.captures.map((capture, idx) => {
        const url = escapeHtml(buildCaptureImageURL(capture, "thumb"));
        const avatarUrl = capture.author_avatar || "/default-avatar.png";
        const authorName = capture.author_name || "Neznámý houbař";
        const accessBadge = buildCaptureAccessBadgeHtml(capture);
        const authorURL = buildPublicProfileURL(capture.author_user_id);
        const species = buildCaptureSpeciesLabel(capture);
        const region = buildCaptureRegionLabel(capture);
        const regionNote = buildCaptureRegionSearchNote(capture);
        const moderatorAction = canModeratorRecheck()
            ? `
                <div class="gallery-item-actions">
                    <button type="button" class="btn btn-secondary gallery-moderator-action" data-capture-id="${escapeHtml(capture.id)}">
                        AI recheck
                    </button>
                </div>
            `
            : "";

        return `
            <div class="gallery-item" data-index="${idx}">
                <div class="gallery-item-header">
                    <a href="${escapeHtml(authorURL)}" class="author-link">
                        <img src="${escapeHtml(avatarUrl)}" class="gallery-item-avatar" alt="Avatar">
                        <span class="gallery-item-author">${escapeHtml(authorName)}</span>
                    </a>
                </div>
                <div class="gallery-item-image">
                    <img src="${url}" loading="lazy" alt="Houbařský úlovek">
                    ${accessBadge}
                </div>
                <div class="gallery-item-copy">
                    ${species ? `<strong class="gallery-item-species">${escapeHtml(species)}</strong>` : ""}
                    ${region ? `<p>${escapeHtml(region)}</p>` : ""}
                    ${regionNote ? `<p>${escapeHtml(regionNote)}</p>` : ""}
                    ${moderatorAction}
                </div>
            </div>
        `;
    }).join("");

    container.querySelectorAll(".gallery-item").forEach((item) => {
        item.addEventListener("click", () => {
            window.currentLightboxIndex = Number(item.dataset.index || 0);
            if (typeof openLightbox === "function") openLightbox();
        });
    });

    container.querySelectorAll(".author-link").forEach((link) => {
        link.addEventListener("click", (event) => {
            event.stopPropagation();
        });
    });

    container.querySelectorAll(".gallery-moderator-action").forEach((button) => {
        button.addEventListener("click", async (event) => {
            event.preventDefault();
            event.stopPropagation();
            await runModeratorRecheck(button.dataset.captureId, button);
        });
    });
}

async function loadGallery({ reset = false } = {}) {
    const container = document.getElementById("gallery-container");
    const loadMoreBtn = document.getElementById("load-more-gallery-btn");
    if (!container || state.isLoading) {
        return;
    }

    state.isLoading = true;
    if (reset) {
        state.page = 1;
        state.hasMore = true;
        state.captures = [];
        container.innerHTML = '<p class="muted-copy" style="grid-column: 1 / -1; text-align: center;">Načítám fotografie...</p>';
        updateGallerySummary();
    }

    try {
        const res = await apiGet(`/api/public/captures?${buildGalleryQuery()}`);
        if (!res || !res.ok) {
            throw new Error("Nepodařilo se načíst galerii.");
        }

        const newCaptures = Array.isArray(res.captures) ? res.captures : [];
        state.hasMore = newCaptures.length === state.pageSize;
        state.captures = reset ? newCaptures : state.captures.concat(newCaptures);

        renderGallery(container);

        if (loadMoreBtn) {
            loadMoreBtn.style.display = state.hasMore ? "inline-block" : "none";
            loadMoreBtn.disabled = false;
        }
    } catch (error) {
        console.error("Failed to load gallery", error);
        if (loadMoreBtn) {
            loadMoreBtn.disabled = false;
        }
        container.innerHTML = `<p class="muted-copy" style="grid-column: 1 / -1; text-align: center;">${escapeHtml(error.message || "Chyba při načítání galerie.")}</p>`;
    } finally {
        state.isLoading = false;
        updateGallerySummary();
    }
}

async function resetGalleryFilters() {
    state.filters = {
        species: "",
        kraj: "",
        okres: "",
        obec: "",
        sort: "published_desc"
    };
    syncGalleryFilterInputs();
    syncGalleryQueryString();
    await loadGallery({ reset: true });
}

async function initGalleryPage() {
    if (document.body.dataset.page !== "gallery") return;

    state.session = await apiGet("/api/session");
    if (state.session && state.session.logged_in) {
        state.me = await apiGet("/api/me");
    }

    setAppIdentity(state.session, state.me);
    renderHeader(state.session, state.me);

    readGalleryFiltersFromQuery();
    syncGalleryFilterInputs();
    syncModeratorModelPanel();

    const loadMoreBtn = document.getElementById("load-more-gallery-btn");
    if (loadMoreBtn) {
        loadMoreBtn.addEventListener("click", async () => {
            if (!state.hasMore || state.isLoading) {
                return;
            }
            loadMoreBtn.disabled = true;
            state.page += 1;
            await loadGallery();
        });
    }

    const filterForm = document.getElementById("gallery-filter-form");
    if (filterForm) {
        filterForm.addEventListener("submit", async (event) => {
            event.preventDefault();
            readGalleryFiltersFromForm();
            syncGalleryQueryString();
            await loadGallery({ reset: true });
        });
    }

    const resetButton = document.getElementById("gallery-filter-reset");
    if (resetButton) {
        resetButton.addEventListener("click", async () => {
            await resetGalleryFilters();
        });
    }

    const moderatorModelSelect = document.getElementById("gallery-moderator-model");
    if (moderatorModelSelect) {
        moderatorModelSelect.addEventListener("change", () => {
            state.moderation.selectedModel = moderatorModelSelect.value || "";
            syncModeratorModelPanel();
        });
    }

    if (canModeratorRecheck()) {
        await loadModeratorModels();
    }

    await loadGallery({ reset: true });
}

document.addEventListener("DOMContentLoaded", initGalleryPage);
