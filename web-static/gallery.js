const state = {
    captures: [],
    page: 1,
    pageSize: 24,
    total: 0,
    totalPages: 0,
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
        loadError: "",
        editorCaptureId: "",
        editorLoading: false,
        editorLoadError: "",
        editorText: "",
        editorNote: ""
    }
};

function parsePositivePage(raw, fallback = 1) {
    const value = Number.parseInt(String(raw || ""), 10);
    if (!Number.isFinite(value) || value <= 0) {
        return fallback;
    }
    return value;
}

function readGalleryFiltersFromQuery() {
    const query = new URLSearchParams(window.location.search);
    state.page = parsePositivePage(query.get("page"), 1);
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
    if (state.page > 1) params.set("page", String(state.page));
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
    params.set("page", String(state.page));
    params.set("page_size", String(state.pageSize));
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

    if (state.totalPages > 1) {
        summary.textContent = `Nalezeno ${state.total} veřejných fotografií. Zobrazuji stranu ${state.page} z ${state.totalPages}.`;
        return;
    }

    summary.textContent = `Nalezeno ${state.total} veřejných fotografií pro aktuální filtr.`;
}

function canModeratorRecheck() {
    return userCanModerateClient(state.me);
}

function buildModeratorTaxonomyLines(species, analysis) {
    const items = Array.isArray(species) ? species.filter(Boolean) : [];
    if (items.length > 0) {
        return items.map((item) => [
            String(item.latin_name || "").trim(),
            String(item.czech_official_name || "").trim(),
            String(item.probability ?? "").trim()
        ].join(" | ")).join("\n");
    }

    const latinName = String(analysis?.primary_latin_name || "").trim();
    const czechName = String(analysis?.primary_czech_official_name || "").trim();
    const probability = Number(analysis?.primary_probability);
    if (!latinName) {
        return "";
    }
    return `${latinName} | ${czechName} | ${Number.isFinite(probability) && probability > 0 ? probability : 0.9}`;
}

function parseModeratorTaxonomyLines(rawText) {
    const lines = String(rawText || "")
        .split("\n")
        .map((line) => line.trim())
        .filter(Boolean);
    if (lines.length === 0) {
        throw new Error("Vyplňte alespoň jeden taxon.");
    }

    return lines.map((line, index) => {
        const parts = line.split("|").map((part) => part.trim());
        const latinName = parts[0] || "";
        const czechName = parts[1] || "";
        const rawProbability = (parts[2] || "").replace("%", "").trim();
        if (!latinName) {
            throw new Error(`Řádek ${index + 1}: latin_name je povinný.`);
        }
        if (!rawProbability) {
            throw new Error(`Řádek ${index + 1}: probability je povinná.`);
        }

        let probability = Number(rawProbability.replace(",", "."));
        if (!Number.isFinite(probability)) {
            throw new Error(`Řádek ${index + 1}: probability musí být číslo.`);
        }
        if (probability > 1 && probability <= 100) {
            probability = probability / 100;
        }
        if (probability <= 0 || probability > 1) {
            throw new Error(`Řádek ${index + 1}: probability musí být mezi 0 a 1 nebo 0 a 100.`);
        }

        return {
            latin_name: latinName,
            czech_official_name: czechName,
            probability
        };
    });
}

function closeModeratorTaxonomyEditor() {
    state.moderation.editorCaptureId = "";
    state.moderation.editorLoading = false;
    state.moderation.editorLoadError = "";
    state.moderation.editorText = "";
    state.moderation.editorNote = "";
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
        closeModeratorTaxonomyEditor();
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

async function openModeratorTaxonomyEditor(captureID) {
    if (!captureID || !canModeratorRecheck()) {
        return;
    }
    if (state.moderation.editorCaptureId === captureID) {
        closeModeratorTaxonomyEditor();
        renderGallery(document.getElementById("gallery-container"));
        return;
    }

    state.moderation.editorCaptureId = captureID;
    state.moderation.editorLoading = true;
    state.moderation.editorLoadError = "";
    state.moderation.editorText = "";
    state.moderation.editorNote = "";
    renderGallery(document.getElementById("gallery-container"));

    try {
        const response = await apiJsonRequest(`/api/moderation/captures/${encodeURIComponent(captureID)}/taxonomy`);
        if (!response || !response.ok) {
            throw new Error("Nepodařilo se načíst současné taxony.");
        }
        state.moderation.editorText = buildModeratorTaxonomyLines(response.species, response.analysis);
    } catch (error) {
        console.error("Failed to load capture taxonomy", error);
        state.moderation.editorLoadError = error.message || "Nepodařilo se načíst současné taxony.";
    } finally {
        state.moderation.editorLoading = false;
        renderGallery(document.getElementById("gallery-container"));
    }
}

async function saveModeratorTaxonomy(captureID, button) {
    if (!captureID || !canModeratorRecheck()) {
        return;
    }

    const textarea = document.getElementById(`gallery-taxonomy-editor-${captureID}`);
    const noteInput = document.getElementById(`gallery-taxonomy-note-${captureID}`);
    let species;
    try {
        species = parseModeratorTaxonomyLines(textarea?.value || "");
    } catch (error) {
        window.alert(error.message || "Taxony se nepodařilo zpracovat.");
        return;
    }

    if (button) {
        button.disabled = true;
    }

    try {
        const response = await apiJsonRequest(`/api/moderation/captures/${encodeURIComponent(captureID)}/taxonomy`, {
            method: "POST",
            body: {
                species,
                note: noteInput?.value || ""
            }
        });
        if (!response || !response.ok) {
            throw new Error("Taxony se nepodařilo uložit.");
        }
        window.alert("Ruční úprava taxonů byla uložena.");
        closeModeratorTaxonomyEditor();
        await loadGallery({ reset: true });
    } catch (error) {
        console.error("Failed to save capture taxonomy", error);
        window.alert(error.message || "Taxony se nepodařilo uložit.");
    } finally {
        if (button) {
            button.disabled = false;
        }
    }
}

function renderModeratorTaxonomyEditor(capture) {
    if (!canModeratorRecheck() || state.moderation.editorCaptureId !== capture.id) {
        return "";
    }

    if (state.moderation.editorLoading) {
        return `
            <div class="gallery-moderator-editor" data-capture-id="${escapeHtml(capture.id)}">
                <p class="muted-copy">Načítám uložené taxony...</p>
            </div>
        `;
    }

    return `
        <div class="gallery-moderator-editor" data-capture-id="${escapeHtml(capture.id)}">
            <label class="field-block">
                <span>Taxony</span>
                <textarea id="gallery-taxonomy-editor-${escapeHtml(capture.id)}" rows="5" placeholder="Boletus edulis | hřib smrkový | 0.96">${escapeHtml(state.moderation.editorText)}</textarea>
            </label>
            <p class="muted-copy">Jeden řádek = <code>latin_name | czech_official_name | probability</code>. Probability může být 0.93 nebo 93.</p>
            ${state.moderation.editorLoadError ? `<p class="status-message is-error">${escapeHtml(state.moderation.editorLoadError)}</p>` : ""}
            <label class="field-block">
                <span>Poznámka moderátora</span>
                <input id="gallery-taxonomy-note-${escapeHtml(capture.id)}" type="text" maxlength="500" value="${escapeHtml(state.moderation.editorNote)}" placeholder="Proč byl taxon upraven">
            </label>
            <div class="gallery-item-actions">
                <button type="button" class="btn btn-secondary gallery-moderator-save-taxonomy-action" data-capture-id="${escapeHtml(capture.id)}">
                    Uložit taxony
                </button>
                <button type="button" class="btn btn-secondary gallery-moderator-cancel-taxonomy-action" data-capture-id="${escapeHtml(capture.id)}">
                    Zavřít editor
                </button>
            </div>
        </div>
    `;
}

async function hideGalleryCapture(captureID, button) {
    if (!captureID || !canModeratorRecheck()) {
        return;
    }
    if (!window.confirm("Opravdu chcete tuto fotografii skrýt z veřejné galerie?")) {
        return;
    }
    const note = window.prompt("Poznámka k moderaci (volitelné):", "");
    if (note === null) {
        return;
    }

    if (button) {
        button.disabled = true;
    }

    try {
        const response = await apiJsonRequest(`/api/moderation/captures/${encodeURIComponent(captureID)}/visibility`, {
            method: "POST",
            body: {
                hidden: true,
                reason_code: "manual_moderation",
                note
            }
        });
        if (!response || !response.ok) {
            throw new Error("Fotografii se nepodařilo skrýt.");
        }
        closeModeratorTaxonomyEditor();
        await loadGallery({ reset: true });
    } catch (error) {
        console.error("Failed to hide capture", error);
        window.alert(error.message || "Fotografii se nepodařilo skrýt.");
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
        const region = buildCaptureKrajLabel(capture);
        const moderatorAction = canModeratorRecheck()
            ? `
                <div class="gallery-item-actions">
                    <button type="button" class="btn btn-secondary gallery-moderator-action" data-capture-id="${escapeHtml(capture.id)}">
                        AI recheck
                    </button>
                    <button type="button" class="btn btn-secondary gallery-moderator-edit-action" data-capture-id="${escapeHtml(capture.id)}">
                        Upravit taxony
                    </button>
                    <button type="button" class="btn btn-secondary gallery-moderator-hide-action" data-capture-id="${escapeHtml(capture.id)}">
                        Skrýt foto
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
                    ${region ? `<p>Kraj: ${escapeHtml(region)}</p>` : ""}
                    ${moderatorAction}
                    ${renderModeratorTaxonomyEditor(capture)}
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

    container.querySelectorAll(".gallery-moderator-edit-action").forEach((button) => {
        button.addEventListener("click", async (event) => {
            event.preventDefault();
            event.stopPropagation();
            await openModeratorTaxonomyEditor(button.dataset.captureId);
        });
    });

    container.querySelectorAll(".gallery-moderator-hide-action").forEach((button) => {
        button.addEventListener("click", async (event) => {
            event.preventDefault();
            event.stopPropagation();
            await hideGalleryCapture(button.dataset.captureId, button);
        });
    });

    container.querySelectorAll(".gallery-moderator-save-taxonomy-action").forEach((button) => {
        button.addEventListener("click", async (event) => {
            event.preventDefault();
            event.stopPropagation();
            await saveModeratorTaxonomy(button.dataset.captureId, button);
        });
    });

    container.querySelectorAll(".gallery-moderator-cancel-taxonomy-action").forEach((button) => {
        button.addEventListener("click", (event) => {
            event.preventDefault();
            event.stopPropagation();
            closeModeratorTaxonomyEditor();
            renderGallery(container);
        });
    });

    container.querySelectorAll(".gallery-moderator-editor").forEach((panel) => {
        panel.addEventListener("click", (event) => {
            event.stopPropagation();
        });
    });
}

// For now the gallery uses numbered pagination instead of incremental "load more".
// If browsing feedback is worse, this is the seam to revert back.
function buildGalleryPaginationItems(currentPage, totalPages) {
    if (totalPages <= 7) {
        return Array.from({ length: totalPages }, (_, idx) => idx + 1);
    }

    const candidates = new Set([
        1,
        totalPages,
        currentPage - 1,
        currentPage,
        currentPage + 1
    ]);
    if (currentPage <= 3) {
        candidates.add(2);
        candidates.add(3);
    }
    if (currentPage >= totalPages - 2) {
        candidates.add(totalPages - 1);
        candidates.add(totalPages - 2);
    }

    const pages = Array.from(candidates)
        .filter((value) => value >= 1 && value <= totalPages)
        .sort((left, right) => left - right);

    const items = [];
    let previous = 0;
    pages.forEach((pageNumber) => {
        if (previous && pageNumber - previous > 1) {
            items.push("gap");
        }
        items.push(pageNumber);
        previous = pageNumber;
    });
    return items;
}

function renderGalleryPagination() {
    const pagination = document.getElementById("gallery-pagination");
    if (!pagination) {
        return;
    }

    if (state.totalPages <= 1) {
        pagination.hidden = true;
        pagination.innerHTML = "";
        return;
    }

    const items = buildGalleryPaginationItems(state.page, state.totalPages);
    pagination.hidden = false;
    pagination.innerHTML = [
        `<button type="button" class="btn btn-secondary" data-page="${state.page - 1}" ${state.page <= 1 ? "disabled" : ""}>Předchozí</button>`,
        ...items.map((item) => {
            if (item === "gap") {
                return '<span class="gallery-pagination-gap" aria-hidden="true">…</span>';
            }
            const active = item === state.page;
            return `
                <button
                    type="button"
                    class="btn ${active ? "btn-primary" : "btn-secondary"}"
                    data-page="${item}"
                    ${active ? "aria-current=\"page\" disabled" : ""}
                >${item}</button>
            `;
        }),
        `<button type="button" class="btn btn-secondary" data-page="${state.page + 1}" ${state.page >= state.totalPages ? "disabled" : ""}>Další</button>`
    ].join("");

    pagination.querySelectorAll("[data-page]").forEach((button) => {
        button.addEventListener("click", async () => {
            const nextPage = parsePositivePage(button.dataset.page, state.page);
            if (nextPage === state.page || nextPage < 1 || nextPage > state.totalPages || state.isLoading) {
                return;
            }
            state.page = nextPage;
            syncGalleryQueryString();
            await loadGallery();
        });
    });
}

async function loadGallery({ reset = false } = {}) {
    const container = document.getElementById("gallery-container");
    if (!container || state.isLoading) {
        return;
    }

    state.isLoading = true;
    if (reset) {
        state.page = 1;
        state.total = 0;
        state.totalPages = 0;
        state.captures = [];
        container.innerHTML = '<p class="muted-copy" style="grid-column: 1 / -1; text-align: center;">Načítám fotografie...</p>';
        updateGallerySummary();
    }

    try {
        const res = await apiGet(`/api/public/captures?${buildGalleryQuery()}`);
        if (!res || !res.ok) {
            throw new Error("Nepodařilo se načíst galerii.");
        }

        state.total = Number.isFinite(Number(res.total)) ? Number(res.total) : 0;
        state.totalPages = Number.isFinite(Number(res.total_pages)) ? Number(res.total_pages) : 0;
        if (state.totalPages > 0 && state.page > state.totalPages) {
            state.page = state.totalPages;
            syncGalleryQueryString();
            state.isLoading = false;
            await loadGallery();
            return;
        }

        state.captures = Array.isArray(res.captures) ? res.captures : [];

        renderGallery(container);
        renderGalleryPagination();
    } catch (error) {
        console.error("Failed to load gallery", error);
        state.total = 0;
        state.totalPages = 0;
        renderGalleryPagination();
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
    state.page = 1;
    syncGalleryFilterInputs();
    syncGalleryQueryString();
    await loadGallery();
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

    const filterForm = document.getElementById("gallery-filter-form");
    if (filterForm) {
        filterForm.addEventListener("submit", async (event) => {
            event.preventDefault();
            readGalleryFiltersFromForm();
            state.page = 1;
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
