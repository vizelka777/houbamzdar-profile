const SERVER_STORAGE_PAGE_SIZE = 12;
const SERVER_STORAGE_REFRESH_MS = 8000;

const storageState = {
    status: "private",
    dateFrom: "",
    dateTo: "",
    page: 1,
    pageSize: SERVER_STORAGE_PAGE_SIZE,
    total: 0,
    totalPages: 0,
    captures: [],
    selectedIds: new Set()
};

let storageRefreshTimer = null;

function clearStorageStatusError() {
    setStatusMessage(document.getElementById("storage-status"), "");
}

function setStorageStatusError(message) {
    setStatusMessage(
        document.getElementById("storage-status"),
        message || "Serverový krok se nepovedl.",
        "error"
    );
}

function showStorageToast(message, options = {}) {
    if (typeof showToast === "function") {
        return showToast(message, options);
    }
    return {
        dismiss() {}
    };
}

function selectedStorageCaptures() {
    return storageState.captures.filter((capture) => storageState.selectedIds.has(capture.id));
}

function buildStorageQuery() {
    const params = new URLSearchParams();
    params.set("page", String(storageState.page));
    params.set("page_size", String(storageState.pageSize));
    params.set("status", storageState.status);
    if (storageState.dateFrom) {
        params.set("date_from", storageState.dateFrom);
    }
    if (storageState.dateTo) {
        params.set("date_to", storageState.dateTo);
    }
    return params.toString();
}

function clearStorageRefreshTimer() {
    if (storageRefreshTimer) {
        window.clearTimeout(storageRefreshTimer);
        storageRefreshTimer = null;
    }
}

function scheduleStorageRefresh() {
    clearStorageRefreshTimer();
    if (storageState.status !== "private") {
        return;
    }
    if (!storageState.captures.some((capture) => capture.publication_review_status === "pending_validation")) {
        return;
    }

    storageRefreshTimer = window.setTimeout(async () => {
        try {
            await loadAndRenderServerStorage();
        } catch (error) {
            console.error("Failed to auto-refresh storage validation state", error);
        }
    }, SERVER_STORAGE_REFRESH_MS);
}

async function fetchServerStoragePage() {
    const result = await apiGet(`/api/captures?${buildStorageQuery()}`);
    if (!result || !result.ok) {
        throw new Error("Nepodařilo se načíst serverový archiv.");
    }

    storageState.captures = result.captures || [];
    storageState.total = result.total || 0;
    storageState.totalPages = result.total_pages || 0;
    storageState.page = result.page || storageState.page;

    if (storageState.totalPages > 0 && storageState.page > storageState.totalPages) {
        storageState.page = storageState.totalPages;
        return fetchServerStoragePage();
    }

    storageState.selectedIds = new Set();
}

function syncStorageToggleButtons() {
    document.querySelectorAll(".server-status-button").forEach((button) => {
        button.classList.toggle("is-active", button.dataset.status === storageState.status);
    });
}

function updateSelectAllState() {
    const selectAll = document.getElementById("storage-select-all");
    if (!selectAll) return;

    const pageCount = storageState.captures.length;
    const selectedCount = storageState.selectedIds.size;

    selectAll.checked = pageCount > 0 && selectedCount === pageCount;
    selectAll.indeterminate = selectedCount > 0 && selectedCount < pageCount;
}

function getStorageApplicableCaptures(action, captures) {
    if (action === "publish") {
        return captures.filter((capture) => capture.status === "private" && capture.publication_review_status !== "pending_validation");
    }
    if (action === "unpublish") {
        return captures.filter((capture) => capture.status === "published");
    }
    return captures;
}

function updateStorageActionState() {
    const selected = selectedStorageCaptures();
    const selectedCount = selected.length;
    const publishButton = document.getElementById("storage-publish-btn");
    const unpublishButton = document.getElementById("storage-unpublish-btn");
    const deleteButton = document.getElementById("storage-delete-btn");
    const selectedCountNode = document.getElementById("storage-selected-count");

    if (selectedCountNode) {
        selectedCountNode.textContent = `Vybráno: ${selectedCount}`;
    }

    const publishVisible = storageState.status === "private";
    const unpublishVisible = storageState.status === "published";

    if (publishButton) {
        publishButton.hidden = !publishVisible;
        publishButton.style.display = publishVisible ? "" : "none";
        publishButton.disabled = getStorageApplicableCaptures("publish", selected).length === 0;
    }
    if (unpublishButton) {
        unpublishButton.hidden = !unpublishVisible;
        unpublishButton.style.display = unpublishVisible ? "" : "none";
        unpublishButton.disabled = getStorageApplicableCaptures("unpublish", selected).length === 0;
    }
    if (deleteButton) {
        deleteButton.disabled = selectedCount === 0;
    }

    updateSelectAllState();
}

function renderStorageSummary() {
    setText("storage-total-count", `Celkem: ${storageState.total}`);
    setText("storage-page-indicator", `Strana ${storageState.page} z ${Math.max(storageState.totalPages, 1)}`);

    const prevButton = document.getElementById("storage-prev-btn");
    const nextButton = document.getElementById("storage-next-btn");
    if (prevButton) {
        prevButton.disabled = storageState.page <= 1;
    }
    if (nextButton) {
        nextButton.disabled = storageState.totalPages === 0 || storageState.page >= storageState.totalPages;
    }
}

function buildStorageStatusBadge(capture) {
    if (capture.status === "published") {
        return "";
    }

    switch (capture.publication_review_status) {
    case "pending_validation":
        return '<span class="status-badge review-pending">Čeká na kontrolu</span>';
    case "rejected":
        return '<span class="status-badge review-error">Zamítnuto</span>';
    case "error":
        return '<span class="status-badge review-error">Chyba kontroly</span>';
    case "approved":
        return '<span class="status-badge verified">Schváleno</span>';
    default:
        return '<span class="status-badge unverified">Nepublikované</span>';
    }
}

function buildStorageReviewPanel(capture) {
    if (!capture || capture.status === "published") {
        return "";
    }

    let title = "";
    let copy = "";
    let copyClass = "";

    switch (capture.publication_review_status) {
    case "pending_validation":
        title = "Kontrola před zveřejněním";
        copy = capture.publication_review_last_error
            ? "Předchozí pokus selhal. Backend zkouší ověření znovu automaticky."
            : "Fotografie čeká na AI kontrolu hub a potvrzení, že souřadnice leží v Česku.";
        copyClass = "is-pending";
        break;
    case "rejected":
        title = "Publikace zamítnuta";
        if (capture.publication_review_reason_code === "missing_coordinates") {
            copy = "Fotografie nemá GPS souřadnice, takže ji nejde veřejně publikovat.";
        } else if (capture.publication_review_reason_code === "outside_czechia") {
            copy = "Souřadnice leží mimo Českou republiku, proto publikace neprošla.";
        } else if (capture.publication_review_reason_code === "no_mushrooms_detected") {
            copy = "AI na snímku nenašla ani jednu houbu, proto publikace neprošla.";
        } else {
            copy = "Fotografie neprošla pravidly pro veřejné zveřejnění.";
        }
        copyClass = "is-error";
        break;
    case "error":
        title = "Kontrolu se nepodařilo dokončit";
        copy = capture.publication_review_last_error
            ? `Poslední chyba: ${capture.publication_review_last_error}`
            : "Ověření publikace zatím skončilo chybou. Zkuste akci spustit znovu.";
        copyClass = "is-error";
        break;
    case "approved":
        title = "Kontrola je hotová";
        copy = "Fotografie už prošla validací a čeká na finální zveřejnění.";
        break;
    default:
        if (!captureHasCoordinates(capture)) {
            title = "Před zveřejněním chybí GPS";
            copy = "Bez souřadnic nepůjde ověřit, jestli je nález z Česka.";
        } else {
            return "";
        }
    }

    return `
        <div class="capture-review-panel">
            <strong class="capture-review-title">${escapeHtml(title)}</strong>
            <p class="capture-review-copy ${copyClass}">${escapeHtml(copy)}</p>
        </div>
    `;
}

function buildStorageMapAction(capture) {
    if (!captureHasCoordinates(capture)) {
        return "";
    }
    return `
        <button
            type="button"
            class="storage-map-btn"
            data-capture-id="${escapeHtml(capture.id)}"
            aria-label="Zobrazit na mapě"
            title="Zobrazit na mapě"
        >
            <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                <path d="M15 5.5 9 3 3 5.5v15L9 18l6 2.5 6-2.5v-15z"></path>
                <path d="M9 3v15"></path>
                <path d="M15 5.5v15"></path>
            </svg>
        </button>
    `;
}

function buildStorageSelectOverlay(capture) {
    return `
        <label class="storage-card-select" aria-label="Vybrat fotografii">
            <input
                class="storage-capture-checkbox storage-card-select-input"
                type="checkbox"
                value="${escapeHtml(capture.id)}"
            >
            <span class="storage-card-select-chip">
                <span class="storage-card-select-check" aria-hidden="true">✓</span>
                <span class="storage-card-select-label">Vybrat</span>
            </span>
        </label>
    `;
}

function buildStorageCoordinatesFreeControl(capture) {
    if (capture.status !== "published" || !captureHasCoordinates(capture)) {
        return "";
    }

    return `
        <label class="capture-free-toggle storage-free-toggle">
            <input
                class="capture-free-checkbox"
                type="checkbox"
                value="${escapeHtml(capture.id)}"
                ${capture.coordinates_free ? "checked" : ""}
            >
            <span>Souřadnice zdarma</span>
        </label>
    `;
}

function renderServerStorageGrid() {
    const grid = document.getElementById("storage-grid");
    if (!grid) return;

    grid.innerHTML = "";

    if (!storageState.captures.length) {
        const emptyState = document.createElement("div");
        emptyState.className = "capture-empty";
        emptyState.textContent = "Pro tento filtr zatím není na serveru žádná fotografie.";
        grid.appendChild(emptyState);
        updateStorageActionState();
        renderStorageSummary();
        scheduleStorageRefresh();
        return;
    }

    storageState.captures.forEach((capture) => {
        const card = document.createElement("article");
        card.className = "capture-item";

        const previewUrl = `${API_URL}/api/captures/${encodeURIComponent(capture.id)}/preview`;
        const mapAction = buildStorageMapAction(capture);
        const coordinatesFreeControl = buildStorageCoordinatesFreeControl(capture);
        const statusBadge = buildStorageStatusBadge(capture);
        const headHtml = [coordinatesFreeControl, statusBadge].filter(Boolean).join("");

        const previewHtml = buildCaptureImageTag(capture, {
            variant: "thumb",
            alt: "Náhled nahrané fotografie",
            className: "capture-thumb storage-thumb",
            loading: "lazy",
            sizes: "(max-width: 720px) 50vw, (max-width: 1080px) 25vw, 180px"
        }) || `<img src="${escapeHtml(previewUrl)}" alt="Náhled nahrané fotografie" class="capture-thumb storage-thumb" loading="lazy">`;

        card.innerHTML = `
            ${headHtml ? `<div class="capture-item-head">${headHtml}</div>` : ""}
            <div class="storage-thumb-shell">
                <button
                    type="button"
                    class="storage-thumb-trigger"
                    data-capture-id="${escapeHtml(capture.id)}"
                    aria-label="Otevřít fotografii"
                >
                    ${previewHtml}
                </button>
                ${buildStorageSelectOverlay(capture)}
                ${mapAction}
            </div>
            <div class="capture-meta">
                <p>${escapeHtml(formatDateTime(capture.captured_at))}</p>
                ${buildStorageReviewPanel(capture)}
            </div>
        `;

        grid.appendChild(card);
    });

    updateStorageActionState();
    renderStorageSummary();
    scheduleStorageRefresh();
}

function handleStorageGridClick(event) {
    const mapButton = event.target.closest(".storage-map-btn");
    if (mapButton) {
        const capture = storageState.captures.find((item) => item.id === mapButton.dataset.captureId);
        if (!capture || !captureHasCoordinates(capture) || typeof openCaptureMapViewer !== "function") {
            return;
        }

        event.preventDefault();
        const opened = openCaptureMapViewer({
            lat: Number(capture.latitude),
            lon: Number(capture.longitude)
        }, capture);
        if (!opened) {
            setStatusMessage(
                document.getElementById("storage-status"),
                "Mapu se nepodařilo otevřít.",
                "error"
            );
        }
        return;
    }

    const thumbTrigger = event.target.closest(".storage-thumb-trigger");
    if (!thumbTrigger) {
        return;
    }

    const captureIndex = storageState.captures.findIndex((item) => item.id === thumbTrigger.dataset.captureId);
    if (captureIndex === -1 || !window.HZDLightbox) {
        return;
    }

    event.preventDefault();
    window.HZDLightbox.openCollection(storageState.captures, captureIndex);
}

async function loadAndRenderServerStorage() {
    await fetchServerStoragePage();
    syncStorageToggleButtons();
    renderServerStorageGrid();
}

async function parseStorageAPIResponse(response, fallbackMessage) {
    const text = await response.text();
    let payload = null;

    if (text) {
        try {
            payload = JSON.parse(text);
        } catch (error) {
            payload = text;
        }
    }

    if (!response.ok) {
        if (payload && typeof payload === "object" && payload.error) {
            throw new Error(payload.error);
        }
        if (typeof payload === "string" && payload.trim()) {
            throw new Error(payload.trim());
        }
        throw new Error(fallbackMessage);
    }

    return payload;
}

async function apiPostStorageAction(captureID, action) {
    const response = await fetch(`${API_URL}/api/captures/${encodeURIComponent(captureID)}/${action}`, {
        method: "POST",
        credentials: "include"
    });

    return parseStorageAPIResponse(response, `Action ${action} failed`);
}

async function apiDeleteStorageCapture(captureID) {
    const response = await fetch(`${API_URL}/api/captures/${encodeURIComponent(captureID)}`, {
        method: "DELETE",
        credentials: "include"
    });

    return parseStorageAPIResponse(response, "Delete failed");
}

async function apiSetCaptureCoordinatesFree(captureID, coordinatesFree) {
    const response = await fetch(`${API_URL}/api/captures/${encodeURIComponent(captureID)}/coordinates-free`, {
        method: "POST",
        credentials: "include",
        headers: {
            "Content-Type": "application/json"
        },
        body: JSON.stringify({
            coordinates_free: coordinatesFree
        })
    });

    return parseStorageAPIResponse(response, "Update of capture coordinate visibility failed");
}

async function performStorageBulkAction(action) {
    const selected = selectedStorageCaptures();
    if (!selected.length) {
        throw new Error("Nejdřív vyberte alespoň jednu fotografii.");
    }

    const applicable = getStorageApplicableCaptures(action, selected);
    if (!applicable.length) {
        throw new Error(
            action === "publish"
                ? "Vybrané fotky už čekají na kontrolu nebo už jsou publikované."
                : action === "unpublish"
                    ? "Vybrané fotky už nejsou publikované."
                    : "Vybrané fotky nejde zpracovat."
        );
    }

    const summary = {
        processed: 0,
        queued: 0,
        published: 0,
        unpublished: 0,
        deleted: 0,
        errors: []
    };

    for (const capture of applicable) {
        try {
            if (action === "delete") {
                await apiDeleteStorageCapture(capture.id);
                summary.deleted += 1;
            } else {
                const response = await apiPostStorageAction(capture.id, action);
                const updatedCapture = response && typeof response === "object" ? response.capture : null;

                if (action === "publish") {
                    if (updatedCapture && updatedCapture.status === "published") {
                        summary.published += 1;
                    } else {
                        summary.queued += 1;
                    }
                }

                if (action === "unpublish") {
                    summary.unpublished += 1;
                }
            }

            summary.processed += 1;
        } catch (error) {
            console.error(`Failed to ${action} capture ${capture.id}`, error);
            summary.errors.push(`${capture.original_file_name || capture.id}: ${error.message || "chyba"}`);
        }
    }

    if (summary.processed === 0 && summary.errors.length) {
        throw new Error(summary.errors.join(" | "));
    }

    return summary;
}

function buildStorageActionMessage(action, summary) {
    if (action === "publish") {
        const parts = [];
        if (summary.queued) {
            parts.push(`Ke kontrole odesláno: ${summary.queued}.`);
        }
        if (summary.published) {
            parts.push(`Okamžitě publikováno: ${summary.published}.`);
        }
        return parts.join(" ");
    }

    if (action === "unpublish") {
        return summary.unpublished ? `Publikace zrušena: ${summary.unpublished}.` : "";
    }

    return summary.deleted ? `Smazáno: ${summary.deleted}.` : "";
}

function buildStorageActionErrorMessage(summary) {
    return Array.isArray(summary?.errors) && summary.errors.length
        ? `Chyby: ${summary.errors.join(" | ")}`
        : "";
}

async function handleStorageGridChange(event) {
    const freeCheckbox = event.target.closest(".capture-free-checkbox");
    if (freeCheckbox) {
        const capture = storageState.captures.find((item) => item.id === freeCheckbox.value);
        if (!capture) return;

        const previousValue = Boolean(capture.coordinates_free);
        const nextValue = Boolean(freeCheckbox.checked);
        const loadingToast = showStorageToast(
            nextValue
                ? "Zpřístupňuji souřadnice zdarma..."
                : "Vrácím souřadnice zpět do houbičkového režimu...",
            { duration: 0 }
        );

        capture.coordinates_free = nextValue;
        freeCheckbox.disabled = true;

        try {
            clearStorageStatusError();

            const res = await apiSetCaptureCoordinatesFree(capture.id, nextValue);
            if (!res || !res.ok || !res.capture) {
                throw new Error("Server nevrátil aktualizovanou fotografii.");
            }

            Object.assign(capture, res.capture);
            renderServerStorageGrid();
            loadingToast.dismiss();
            showStorageToast(
                nextValue
                    ? "Souřadnice jsou teď zdarma pro všechny a funguje i hledání po okresu nebo obci."
                    : "Souřadnice jsou znovu chráněné houbičkou a veřejně se hledá jen podle kraje.",
                { kind: "success" }
            );
        } catch (error) {
            console.error("Failed to update capture coordinates_free", error);
            loadingToast.dismiss();
            capture.coordinates_free = previousValue;
            freeCheckbox.checked = previousValue;
            setStorageStatusError(error.message || "Nepodařilo se změnit přístup k souřadnicím.");
        } finally {
            freeCheckbox.disabled = false;
        }
        return;
    }

    const checkbox = event.target.closest(".storage-capture-checkbox");
    if (!checkbox) return;

    if (checkbox.checked) {
        storageState.selectedIds.add(checkbox.value);
    } else {
        storageState.selectedIds.delete(checkbox.value);
    }
    updateStorageActionState();
}

function handleStorageSelectAll(event) {
    const checked = event.target.checked;
    storageState.selectedIds = new Set(
        checked ? storageState.captures.map((capture) => capture.id) : []
    );

    document.querySelectorAll(".storage-capture-checkbox").forEach((checkbox) => {
        checkbox.checked = checked;
    });

    updateStorageActionState();
}

async function runStorageAction(action, busyMessage) {
    const loadingToast = showStorageToast(busyMessage, { duration: 0 });

    try {
        clearStorageStatusError();
        const summary = await performStorageBulkAction(action);
        await loadAndRenderServerStorage();
        loadingToast.dismiss();

        const successMessage = buildStorageActionMessage(action, summary);
        if (successMessage) {
            showStorageToast(successMessage, { kind: "success" });
        }

        const errorMessage = buildStorageActionErrorMessage(summary);
        if (errorMessage) {
            setStorageStatusError(errorMessage);
        } else {
            clearStorageStatusError();
        }
    } catch (error) {
        console.error(`Failed to ${action} storage captures`, error);
        loadingToast.dismiss();
        setStorageStatusError(error.message || "Serverový krok se nepovedl.");
    }
}

async function initServerStoragePage() {
    if (document.body.dataset.page !== "server-storage") return;

    const session = await apiGet("/api/session");
    if (!session || !session.logged_in) {
        window.location.href = "/";
        return;
    }

    const me = await apiGet("/api/me");
    if (!me) return;

    setAppIdentity(session, me);
    renderHeader(session, me);

    const grid = document.getElementById("storage-grid");
    const filterForm = document.getElementById("storage-filter-form");
    const dateFromInput = document.getElementById("storage-date-from-input");
    const dateToInput = document.getElementById("storage-date-to-input");
    const selectAll = document.getElementById("storage-select-all");
    const prevButton = document.getElementById("storage-prev-btn");
    const nextButton = document.getElementById("storage-next-btn");
    const publishButton = document.getElementById("storage-publish-btn");
    const unpublishButton = document.getElementById("storage-unpublish-btn");
    const deleteButton = document.getElementById("storage-delete-btn");

    document.querySelectorAll(".server-status-button").forEach((button) => {
        button.addEventListener("click", async () => {
            storageState.status = button.dataset.status || "private";
            storageState.page = 1;
            const loadingToast = showStorageToast("Načítám archiv...", { duration: 0 });
            try {
                clearStorageStatusError();
                await loadAndRenderServerStorage();
                loadingToast.dismiss();
            } catch (error) {
                console.error("Failed to switch storage status filter", error);
                loadingToast.dismiss();
                setStorageStatusError("Archiv se nepodařilo načíst.");
            }
        });
    });

    const applyStorageDateFilter = async () => {
        const nextDateFrom = dateFromInput.value || "";
        const nextDateTo = dateToInput.value || "";

        if (nextDateFrom && nextDateTo && nextDateFrom > nextDateTo) {
            setStorageStatusError("Datum Od musí být dříve nebo stejné jako Datum Do.");
            return;
        }

        storageState.dateFrom = nextDateFrom;
        storageState.dateTo = nextDateTo;
        storageState.page = 1;
        const loadingToast = showStorageToast("Hledám podle období...", { duration: 0 });

        try {
            clearStorageStatusError();
            await loadAndRenderServerStorage();
            loadingToast.dismiss();
        } catch (error) {
            console.error("Failed to filter server archive by date range", error);
            loadingToast.dismiss();
            setStorageStatusError("Vyhledávání podle období se nepovedlo.");
        }
    };

    if (filterForm) {
        filterForm.addEventListener("submit", (event) => {
            event.preventDefault();
        });
    }
    [dateFromInput, dateToInput].forEach((input) => {
        input?.addEventListener("change", applyStorageDateFilter);
    });

    selectAll.addEventListener("change", handleStorageSelectAll);
    grid.addEventListener("change", handleStorageGridChange);
    grid.addEventListener("click", handleStorageGridClick);

    prevButton.addEventListener("click", async () => {
        if (storageState.page <= 1) return;
        storageState.page -= 1;
        const loadingToast = showStorageToast("Načítám předchozí stránku...", { duration: 0 });
        try {
            clearStorageStatusError();
            await loadAndRenderServerStorage();
            loadingToast.dismiss();
        } catch (error) {
            console.error("Failed to load previous server archive page", error);
            loadingToast.dismiss();
            setStorageStatusError("Předchozí stránku se nepodařilo načíst.");
        }
    });

    nextButton.addEventListener("click", async () => {
        if (storageState.totalPages === 0 || storageState.page >= storageState.totalPages) return;
        storageState.page += 1;
        const loadingToast = showStorageToast("Načítám další stránku...", { duration: 0 });
        try {
            clearStorageStatusError();
            await loadAndRenderServerStorage();
            loadingToast.dismiss();
        } catch (error) {
            console.error("Failed to load next server archive page", error);
            loadingToast.dismiss();
            setStorageStatusError("Další stránku se nepodařilo načíst.");
        }
    });

    publishButton.addEventListener("click", async () => {
        await runStorageAction("publish", "Spouštím publikaci vybraných fotografií...");
    });

    unpublishButton.addEventListener("click", async () => {
        await runStorageAction("unpublish", "Ruším publikaci vybraných fotografií...");
    });

    deleteButton.addEventListener("click", async () => {
        await runStorageAction("delete", "Mažu vybrané fotografie ze serveru...");
    });

    window.addEventListener("beforeunload", clearStorageRefreshTimer);

    const loadingToast = showStorageToast("Načítám archiv...", { duration: 0 });
    try {
        clearStorageStatusError();
        await loadAndRenderServerStorage();
        loadingToast.dismiss();
    } catch (error) {
        console.error("Failed to initialize server storage page", error);
        loadingToast.dismiss();
        setStorageStatusError("Archiv se nepodařilo načíst.");
    }
}

document.addEventListener("DOMContentLoaded", initServerStoragePage);
