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
        modalCaptureId: "",
        editorCaptureId: "",
        editorLoading: false,
        editorLoadError: "",
        editorText: "",
        editorNote: "",
        geoEditorCaptureId: "",
        geoEditorLoading: false,
        geoEditorLoadError: "",
        geoEditorCountryCode: "",
        geoEditorKrajName: "",
        geoEditorOkresName: "",
        geoEditorObecName: "",
        geoEditorCanViewDetailed: false,
        geoEditorNote: ""
    }
};

const GALLERY_SORT_OPTIONS = new Set([
    "published_desc",
    "captured_desc",
    "probability_desc",
    "kraj_asc"
]);

function normalizeGallerySort(rawValue) {
    const value = String(rawValue || "").trim();
    if (!GALLERY_SORT_OPTIONS.has(value)) {
        return "published_desc";
    }
    return value;
}

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
    state.filters.sort = normalizeGallerySort(query.get("sort"));
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
    state.filters.sort = normalizeGallerySort(readValue("gallery-filter-sort"));
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

function closeModeratorGeoEditor() {
    state.moderation.geoEditorCaptureId = "";
    state.moderation.geoEditorLoading = false;
    state.moderation.geoEditorLoadError = "";
    state.moderation.geoEditorCountryCode = "";
    state.moderation.geoEditorKrajName = "";
    state.moderation.geoEditorOkresName = "";
    state.moderation.geoEditorObecName = "";
    state.moderation.geoEditorCanViewDetailed = false;
    state.moderation.geoEditorNote = "";
}

function findGalleryCapture(captureID) {
    if (!captureID) {
        return null;
    }
    return state.captures.find((item) => item && item.id === captureID) || null;
}

function getActiveModerationCapture() {
    return findGalleryCapture(state.moderation.modalCaptureId);
}

function closeGallerySpeciesModal() {
    const modal = document.getElementById("gallery-species-modal");
    if (!modal) {
        return;
    }
    modal.hidden = true;
    modal.setAttribute("aria-hidden", "true");
}

function openGallerySpeciesModal(captureID) {
    const modal = document.getElementById("gallery-species-modal");
    const body = document.getElementById("gallery-species-body");
    const meta = document.getElementById("gallery-species-meta");
    if (!modal || !body || !meta) {
        return;
    }

    const capture = state.captures.find((item) => item && item.id === captureID);
    const entries = buildCaptureSpeciesEntries(capture);
    if (!capture || entries.length === 0) {
        return;
    }

    const authorName = String(capture.author_name || "Neznámý houbař").trim();
    const region = buildCaptureKrajLabel(capture);
    meta.innerHTML = [
        authorName ? `<span>${escapeHtml(authorName)}</span>` : "",
        region ? `<span>${escapeHtml(region)}</span>` : ""
    ].filter(Boolean).join(" • ");
    body.innerHTML = `
        <ul class="capture-species-list">
            ${entries.map((entry) => `<li>${escapeHtml(entry)}</li>`).join("")}
        </ul>
    `;

    modal.hidden = false;
    modal.setAttribute("aria-hidden", "false");
}

function renderGalleryModerationBody(capture) {
    if (!capture) {
        return `
            <div class="help-panel gallery-moderation-empty">
                <p class="muted-copy">Fotografie pro moderaci už není v aktuálním seznamu.</p>
            </div>
        `;
    }

    if (state.moderation.editorCaptureId === capture.id) {
        return renderModeratorTaxonomyEditor(capture);
    }
    if (state.moderation.geoEditorCaptureId === capture.id) {
        return renderModeratorGeoEditor(capture);
    }

    const modelLabel = state.moderation.selectedModel || state.moderation.defaultModel || "výchozí model backendu";
    return `
        <div class="help-panel gallery-moderation-empty">
            <p class="section-label">Rychlá moderace</p>
            <p>Vyberte akci nahoře. AI recheck použije model <strong>${escapeHtml(modelLabel)}</strong>.</p>
            <p class="muted-copy">Taxony i lokaci lze potom ručně opravit a uložit bez zásahu do backendu.</p>
        </div>
    `;
}

function syncGalleryModerationModal() {
    const modal = document.getElementById("gallery-moderation-modal");
    const meta = document.getElementById("gallery-moderation-meta");
    const preview = document.getElementById("gallery-moderation-preview");
    const body = document.getElementById("gallery-moderation-body");
    const aiButton = document.getElementById("gallery-moderation-ai-action");
    const taxonomyButton = document.getElementById("gallery-moderation-taxonomy-action");
    const geoButton = document.getElementById("gallery-moderation-geo-action");
    const hideButton = document.getElementById("gallery-moderation-hide-action");
    const capture = getActiveModerationCapture();

    if (!modal || !meta || !preview || !body || !aiButton || !taxonomyButton || !geoButton || !hideButton) {
        return;
    }

    if (!canModeratorRecheck() || !capture) {
        modal.hidden = true;
        modal.setAttribute("aria-hidden", "true");
        body.innerHTML = "";
        preview.removeAttribute("src");
        preview.alt = "";
        return;
    }

    const authorName = String(capture.author_name || "Neznámý houbař").trim();
    const region = buildCaptureRegionLabel(capture);
    meta.innerHTML = [
        authorName ? `<span>${escapeHtml(authorName)}</span>` : "",
        region ? `<span>${escapeHtml(region)}</span>` : "",
        capture.id ? `<span>ID ${escapeHtml(capture.id)}</span>` : ""
    ].filter(Boolean).join(" • ");
    setCaptureImageElement(preview, capture, {
        variant: "thumb",
        alt: authorName || "Fotografie",
        loading: "lazy",
        sizes: "384px"
    });
    body.innerHTML = renderGalleryModerationBody(capture);

    aiButton.dataset.captureId = capture.id;
    taxonomyButton.dataset.captureId = capture.id;
    geoButton.dataset.captureId = capture.id;
    hideButton.dataset.captureId = capture.id;

    modal.hidden = false;
    modal.setAttribute("aria-hidden", "false");
}

function openGalleryModerationModal(captureID) {
    if (!captureID || !canModeratorRecheck()) {
        return;
    }
    state.moderation.modalCaptureId = captureID;
    closeModeratorTaxonomyEditor();
    closeModeratorGeoEditor();
    syncGalleryModerationModal();
}

function closeGalleryModerationModal() {
    state.moderation.modalCaptureId = "";
    closeModeratorTaxonomyEditor();
    closeModeratorGeoEditor();
    syncGalleryModerationModal();
}

function buildGallerySpeciesButton(capture) {
    const entries = buildCaptureSpeciesEntries(capture);
    if (entries.length === 0) {
        return "";
    }
    return `
        <button
            type="button"
            class="gallery-species-trigger"
            data-capture-id="${escapeHtml(capture.id)}"
            aria-label="Zobrazit rozpoznané druhy"
        >
            <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                <path d="M7 5.5A2.5 2.5 0 0 1 9.5 3H19v18h-9.5A2.5 2.5 0 0 0 7 23z"></path>
                <path d="M7 5.5A2.5 2.5 0 0 0 4.5 3H5v18h.5A2.5 2.5 0 0 1 8 23"></path>
                <path d="M10.5 8H16"></path>
                <path d="M10.5 11.5H16"></path>
                <path d="M10.5 15H14.5"></path>
            </svg>
            <span class="sr-only">Zobrazit rozpoznané druhy</span>
        </button>
    `;
}

function formatGalleryRegionLabel(region) {
    const safeRegion = escapeHtml(region || "");
    return safeRegion.replace(/\s+([^\s]+)$/u, "<br>$1");
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
        closeModeratorGeoEditor();
        await loadGallery({ reset: true });
        syncGalleryModerationModal();
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
    state.moderation.modalCaptureId = captureID;
    closeModeratorGeoEditor();
    state.moderation.editorCaptureId = captureID;
    state.moderation.editorLoading = true;
    state.moderation.editorLoadError = "";
    state.moderation.editorText = "";
    state.moderation.editorNote = "";
    syncGalleryModerationModal();

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
        syncGalleryModerationModal();
    }
}

async function openModeratorGeoEditor(captureID) {
    if (!captureID || !canModeratorRecheck()) {
        return;
    }
    state.moderation.modalCaptureId = captureID;
    closeModeratorTaxonomyEditor();
    state.moderation.geoEditorCaptureId = captureID;
    state.moderation.geoEditorLoading = true;
    state.moderation.geoEditorLoadError = "";
    state.moderation.geoEditorCountryCode = "";
    state.moderation.geoEditorKrajName = "";
    state.moderation.geoEditorOkresName = "";
    state.moderation.geoEditorObecName = "";
    state.moderation.geoEditorCanViewDetailed = false;
    state.moderation.geoEditorNote = "";
    syncGalleryModerationModal();

    try {
        const response = await apiJsonRequest(`/api/moderation/captures/${encodeURIComponent(captureID)}/geo`);
        if (!response || !response.ok) {
            throw new Error("Nepodařilo se načíst uloženou lokalitu.");
        }
        const capture = response.capture || {};
        const geo = response.geo || {};
        state.moderation.geoEditorCountryCode = String(geo.country_code || "").trim();
        state.moderation.geoEditorKrajName = String(geo.kraj_name || "").trim();
        state.moderation.geoEditorOkresName = String(geo.okres_name || "").trim();
        state.moderation.geoEditorObecName = String(geo.obec_name || "").trim();
        state.moderation.geoEditorCanViewDetailed = Boolean(capture.can_view_detailed_location);
    } catch (error) {
        console.error("Failed to load capture geo", error);
        state.moderation.geoEditorLoadError = error.message || "Nepodařilo se načíst uloženou lokalitu.";
    } finally {
        state.moderation.geoEditorLoading = false;
        syncGalleryModerationModal();
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
        syncGalleryModerationModal();
    } catch (error) {
        console.error("Failed to save capture taxonomy", error);
        window.alert(error.message || "Taxony se nepodařilo uložit.");
    } finally {
        if (button) {
            button.disabled = false;
        }
    }
}

async function saveModeratorGeo(captureID, button) {
    if (!captureID || !canModeratorRecheck()) {
        return;
    }

    const countryInput = document.getElementById(`gallery-geo-country-${captureID}`);
    const krajInput = document.getElementById(`gallery-geo-kraj-${captureID}`);
    const okresInput = document.getElementById(`gallery-geo-okres-${captureID}`);
    const obecInput = document.getElementById(`gallery-geo-obec-${captureID}`);
    const noteInput = document.getElementById(`gallery-geo-note-${captureID}`);

    const body = {
        country_code: countryInput?.value || "",
        kraj_name: krajInput?.value || "",
        note: noteInput?.value || ""
    };
    if (state.moderation.geoEditorCanViewDetailed) {
        body.okres_name = okresInput?.value || "";
        body.obec_name = obecInput?.value || "";
    }

    if (button) {
        button.disabled = true;
    }

    try {
        const response = await apiJsonRequest(`/api/moderation/captures/${encodeURIComponent(captureID)}/geo`, {
            method: "POST",
            body
        });
        if (!response || !response.ok) {
            throw new Error("Lokalitu se nepodařilo uložit.");
        }
        window.alert("Ruční úprava lokality byla uložena.");
        closeModeratorGeoEditor();
        await loadGallery({ reset: true });
        syncGalleryModerationModal();
    } catch (error) {
        console.error("Failed to save capture geo", error);
        window.alert(error.message || "Lokalitu se nepodařilo uložit.");
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

function renderModeratorGeoEditor(capture) {
    if (!canModeratorRecheck() || state.moderation.geoEditorCaptureId !== capture.id) {
        return "";
    }

    if (state.moderation.geoEditorLoading) {
        return `
            <div class="gallery-moderator-editor" data-capture-id="${escapeHtml(capture.id)}">
                <p class="muted-copy">Načítám uloženou lokalitu...</p>
            </div>
        `;
    }

    const canViewDetailed = state.moderation.geoEditorCanViewDetailed;
    return `
        <div class="gallery-moderator-editor" data-capture-id="${escapeHtml(capture.id)}">
            <label class="field-block">
                <span>Země</span>
                <input id="gallery-geo-country-${escapeHtml(capture.id)}" type="text" maxlength="8" value="${escapeHtml(state.moderation.geoEditorCountryCode)}" placeholder="CZ">
            </label>
            <label class="field-block">
                <span>Kraj</span>
                <input id="gallery-geo-kraj-${escapeHtml(capture.id)}" type="text" maxlength="160" value="${escapeHtml(state.moderation.geoEditorKrajName)}" placeholder="Jihomoravský kraj">
            </label>
            ${canViewDetailed ? `
                <label class="field-block">
                    <span>Okres</span>
                    <input id="gallery-geo-okres-${escapeHtml(capture.id)}" type="text" maxlength="160" value="${escapeHtml(state.moderation.geoEditorOkresName)}" placeholder="Brno-venkov">
                </label>
                <label class="field-block">
                    <span>Obec</span>
                    <input id="gallery-geo-obec-${escapeHtml(capture.id)}" type="text" maxlength="160" value="${escapeHtml(state.moderation.geoEditorObecName)}" placeholder="Lomnice">
                </label>
            ` : `<p class="muted-copy">Nižší lokalitu lze upravit až poté, co ji tento účet získá běžným odemčením souřadnic.</p>`}
            ${state.moderation.geoEditorLoadError ? `<p class="status-message is-error">${escapeHtml(state.moderation.geoEditorLoadError)}</p>` : ""}
            <label class="field-block">
                <span>Poznámka moderátora</span>
                <input id="gallery-geo-note-${escapeHtml(capture.id)}" type="text" maxlength="500" value="${escapeHtml(state.moderation.geoEditorNote)}" placeholder="Proč byla lokalita upravena">
            </label>
            <div class="gallery-item-actions">
                <button type="button" class="btn btn-secondary gallery-moderator-save-geo-action" data-capture-id="${escapeHtml(capture.id)}">
                    Uložit lokalitu
                </button>
                <button type="button" class="btn btn-secondary gallery-moderator-cancel-geo-action" data-capture-id="${escapeHtml(capture.id)}">
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
        closeModeratorGeoEditor();
        await loadGallery({ reset: true });
        syncGalleryModerationModal();
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
        container.innerHTML = '<p class="muted-copy gallery-grid-status">Zatím nejsou sdíleny žádné fotografie pro tento filtr.</p>';
        return;
    }

    container.innerHTML = state.captures.map((capture, idx) => {
        const avatarUrl = capture.author_avatar || "/default-avatar.png";
        const authorName = capture.author_name || "Neznámý houbař";
        const accessBadge = buildCaptureAccessBadgeHtml(capture);
        const authorURL = buildPublicProfileURL(capture.author_user_id);
        const region = buildCaptureKrajLabel(capture);
        const imageHtml = buildCaptureImageTag(capture, {
            variant: "thumb",
            alt: "Houbařský úlovek",
            loading: "lazy",
            sizes: "(max-width: 720px) 50vw, (max-width: 1200px) 33vw, 384px"
        });
        const speciesButton = buildGallerySpeciesButton(capture);
        const moderatorTrigger = canModeratorRecheck()
            ? `
                <button type="button" class="gallery-moderation-trigger" data-capture-id="${escapeHtml(capture.id)}" aria-label="Otevřít moderaci fotografie">
                    <span aria-hidden="true">M</span>
                </button>
            `
            : "";
        const moderatorCardClass = canModeratorRecheck() ? " gallery-item--moderator" : "";

        return `
            <div class="gallery-item${moderatorCardClass}" data-index="${idx}" tabindex="0" role="button" aria-label="Zobrazit detail fotky">
                <div class="gallery-item-header">
                    <a href="${escapeHtml(authorURL)}" class="author-link">
                        <img src="${escapeHtml(avatarUrl)}" class="gallery-item-avatar" alt="Avatar">
                        <span class="gallery-item-author">${escapeHtml(authorName)}</span>
                    </a>
                </div>
                <div class="gallery-item-image">
                    ${imageHtml}
                    ${accessBadge}
                </div>
                <div class="gallery-item-copy">
                    ${region ? `
                        <div class="gallery-item-meta-row">
                            <p class="gallery-item-region">${formatGalleryRegionLabel(region)}</p>
                        </div>
                    ` : ""}
                </div>
                ${moderatorTrigger}
                ${speciesButton}
            </div>
        `;
    }).join("");

    container.querySelectorAll(".gallery-item").forEach((item) => {
        const openItemLightbox = () => {
            if (!window.HZDLightbox) {
                return;
            }
            window.HZDLightbox.openCollection(state.captures, Number(item.dataset.index || 0));
        };
        item.addEventListener("click", openItemLightbox);
        item.addEventListener("keydown", (e) => {
            if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                openItemLightbox();
            }
        });
    });

    container.querySelectorAll(".author-link").forEach((link) => {
        link.addEventListener("click", (event) => {
            event.stopPropagation();
        });
    });

    container.querySelectorAll(".gallery-species-trigger").forEach((button) => {
        button.addEventListener("click", (event) => {
            event.preventDefault();
            event.stopPropagation();
            openGallerySpeciesModal(button.dataset.captureId);
        });
    });

    container.querySelectorAll(".gallery-moderation-trigger").forEach((button) => {
        button.addEventListener("click", (event) => {
            event.preventDefault();
            event.stopPropagation();
            openGalleryModerationModal(button.dataset.captureId);
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
        container.innerHTML = '<p class="muted-copy gallery-grid-status">Načítám fotografie...</p>';
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
        container.innerHTML = `<p class="muted-copy gallery-grid-status">${escapeHtml(error.message || "Chyba při načítání galerie.")}</p>`;
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

function collapseGalleryFiltersOnMobile() {
    const overview = document.getElementById("gallery-overview");
    if (!(overview instanceof HTMLDetailsElement)) {
        return;
    }

    if (window.matchMedia("(min-width: 720px)").matches) {
        return;
    }

    overview.open = false;
    overview.scrollIntoView({ block: "start", behavior: "smooth" });
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
            collapseGalleryFiltersOnMobile();
        });
    }

    const resetButton = document.getElementById("gallery-filter-reset");
    if (resetButton) {
        resetButton.addEventListener("click", async () => {
            await resetGalleryFilters();
        });
    }

    const speciesModal = document.getElementById("gallery-species-modal");
    const speciesModalClose = document.getElementById("gallery-species-close");
    const moderationModal = document.getElementById("gallery-moderation-modal");
    const moderationModalClose = document.getElementById("gallery-moderation-close");
    if (speciesModal) {
        speciesModal.addEventListener("click", (event) => {
            if (event.target instanceof HTMLElement && event.target.hasAttribute("data-close-species-modal")) {
                closeGallerySpeciesModal();
            }
        });
    }
    if (speciesModalClose) {
        speciesModalClose.addEventListener("click", closeGallerySpeciesModal);
    }
    if (moderationModal) {
        moderationModal.addEventListener("click", async (event) => {
            const target = event.target instanceof HTMLElement ? event.target : null;
            if (!target) {
                return;
            }
            if (target.hasAttribute("data-close-gallery-moderation")) {
                closeGalleryModerationModal();
                return;
            }

            const button = target.closest("button");
            if (!button) {
                return;
            }

            const captureID = button.dataset.captureId || state.moderation.modalCaptureId;
            if (!captureID) {
                return;
            }

            if (button.id === "gallery-moderation-ai-action") {
                await runModeratorRecheck(captureID, button);
                return;
            }
            if (button.id === "gallery-moderation-taxonomy-action") {
                await openModeratorTaxonomyEditor(captureID);
                return;
            }
            if (button.id === "gallery-moderation-geo-action") {
                await openModeratorGeoEditor(captureID);
                return;
            }
            if (button.id === "gallery-moderation-hide-action") {
                await hideGalleryCapture(captureID, button);
                return;
            }
            if (button.classList.contains("gallery-moderator-save-taxonomy-action")) {
                await saveModeratorTaxonomy(captureID, button);
                return;
            }
            if (button.classList.contains("gallery-moderator-cancel-taxonomy-action")) {
                closeModeratorTaxonomyEditor();
                syncGalleryModerationModal();
                return;
            }
            if (button.classList.contains("gallery-moderator-save-geo-action")) {
                await saveModeratorGeo(captureID, button);
                return;
            }
            if (button.classList.contains("gallery-moderator-cancel-geo-action")) {
                closeModeratorGeoEditor();
                syncGalleryModerationModal();
            }
        });
    }
    if (moderationModalClose) {
        moderationModalClose.addEventListener("click", closeGalleryModerationModal);
    }
    window.addEventListener("keydown", (event) => {
        if (event.key === "Escape") {
            closeGallerySpeciesModal();
            closeGalleryModerationModal();
        }
    });

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
