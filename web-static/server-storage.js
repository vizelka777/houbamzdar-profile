const SERVER_STORAGE_PAGE_SIZE = 12;
const SERVER_STORAGE_REFRESH_MS = 8000;

const storageState = {
    status: "private",
    capturedOn: "",
    page: 1,
    pageSize: SERVER_STORAGE_PAGE_SIZE,
    total: 0,
    totalPages: 0,
    captures: [],
    selectedIds: new Set()
};

let storageRefreshTimer = null;

function formatStorageCoords(lat, lng) {
    if (typeof lat !== "number" || typeof lng !== "number") {
        return "Bez GPS";
    }
    return `${lat.toFixed(5)}, ${lng.toFixed(5)}`;
}

function selectedStorageCaptures() {
    return storageState.captures.filter((capture) => storageState.selectedIds.has(capture.id));
}

function buildStorageQuery() {
    const params = new URLSearchParams();
    params.set("page", String(storageState.page));
    params.set("page_size", String(storageState.pageSize));
    params.set("status", storageState.status);
    if (storageState.capturedOn) {
        params.set("captured_on", storageState.capturedOn);
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

    if (publishButton) {
        publishButton.disabled = getStorageApplicableCaptures("publish", selected).length === 0;
    }
    if (unpublishButton) {
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
        return '<span class="status-badge verified">Veřejné</span>';
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
        return '<span class="status-badge unverified">Soukromé</span>';
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
            title = "Připraveno ke kontrole";
            copy = "Po kliknutí na zveřejnění backend nejdřív ověří Česko a přítomnost houby.";
        }
    }

    return `
        <div class="capture-review-panel">
            <strong class="capture-review-title">${escapeHtml(title)}</strong>
            <p class="capture-review-copy ${copyClass}">${escapeHtml(copy)}</p>
        </div>
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
        const publicLink = capture.public_url
            ? `<a href="${escapeHtml(capture.public_url)}" target="_blank" rel="noreferrer" class="capture-link">Otevřít veřejnou verzi</a>`
            : "";
        const coordinatesAccessHtml = captureHasCoordinates(capture)
            ? `
                <div class="capture-free-panel">
                    <label class="capture-free-toggle">
                        <input
                            class="capture-free-checkbox"
                            type="checkbox"
                            value="${escapeHtml(capture.id)}"
                            ${capture.coordinates_free ? "checked" : ""}
                        >
                        <span>Souřadnice zdarma</span>
                    </label>
                    <p class="capture-free-help">
                        ${capture.coordinates_free
                            ? "Veřejné hledání může jít až po okres nebo obec."
                            : "Veřejné hledání zůstane jen na úrovni kraje, přesná poloha je za houbičku."}
                    </p>
                </div>
            `
            : '<p class="capture-free-help">Bez GPS, není co zpřístupnit.</p>';

        card.innerHTML = `
            <div class="capture-item-head">
                <label class="capture-select">
                    <input class="storage-capture-checkbox" type="checkbox" value="${escapeHtml(capture.id)}">
                    <span>Vybrat</span>
                </label>
                ${buildStorageStatusBadge(capture)}
            </div>
            <img src="${escapeHtml(previewUrl)}" alt="Soukromý náhled nálezu" class="capture-thumb" loading="lazy">
            <div class="capture-meta">
                <h3>${escapeHtml(capture.original_file_name || "Nález")}</h3>
                <p>${escapeHtml(formatDateTime(capture.captured_at))}</p>
                <p>${escapeHtml(formatStorageCoords(capture.latitude, capture.longitude))}</p>
                <p>${escapeHtml(`${Math.round((capture.size_bytes || 0) / 1024)} KB`)}</p>
                ${publicLink}
                ${coordinatesAccessHtml}
                ${buildStorageReviewPanel(capture)}
            </div>
        `;

        grid.appendChild(card);
    });

    updateStorageActionState();
    renderStorageSummary();
    scheduleStorageRefresh();
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
                ? "Vybrané fotky už čekají na kontrolu nebo nejsou soukromé."
                : action === "unpublish"
                    ? "Vybrané fotky už nejsou veřejné."
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
            parts.push(`Okamžitě zveřejněno: ${summary.published}.`);
        }
        if (summary.errors.length) {
            parts.push(`Chyby: ${summary.errors.join(" | ")}`);
        }
        return parts.join(" ");
    }

    if (action === "unpublish") {
        const parts = [`Staženo z webu: ${summary.unpublished}.`];
        if (summary.errors.length) {
            parts.push(`Chyby: ${summary.errors.join(" | ")}`);
        }
        return parts.join(" ");
    }

    const parts = [`Smazáno: ${summary.deleted}.`];
    if (summary.errors.length) {
        parts.push(`Chyby: ${summary.errors.join(" | ")}`);
    }
    return parts.join(" ");
}

async function handleStorageGridChange(event) {
    const freeCheckbox = event.target.closest(".capture-free-checkbox");
    if (freeCheckbox) {
        const statusNode = document.getElementById("storage-status");
        const capture = storageState.captures.find((item) => item.id === freeCheckbox.value);
        if (!capture) return;

        const previousValue = Boolean(capture.coordinates_free);
        const nextValue = Boolean(freeCheckbox.checked);

        capture.coordinates_free = nextValue;
        freeCheckbox.disabled = true;

        try {
            setStatusMessage(
                statusNode,
                nextValue
                    ? "Zpřístupňuji souřadnice zdarma..."
                    : "Vrácím souřadnice zpět do houbičkového režimu..."
            );

            const res = await apiSetCaptureCoordinatesFree(capture.id, nextValue);
            if (!res || !res.ok || !res.capture) {
                throw new Error("Server nevrátil aktualizovanou fotografii.");
            }

            Object.assign(capture, res.capture);
            renderServerStorageGrid();
            setStatusMessage(
                statusNode,
                nextValue
                    ? "Souřadnice jsou teď zdarma pro všechny a funguje i hledání po okresu nebo obci."
                    : "Souřadnice jsou znovu chráněné houbičkou a veřejně se hledá jen podle kraje.",
                "success"
            );
        } catch (error) {
            console.error("Failed to update capture coordinates_free", error);
            capture.coordinates_free = previousValue;
            freeCheckbox.checked = previousValue;
            setStatusMessage(statusNode, error.message || "Nepodařilo se změnit přístup k souřadnicím.", "error");
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
    const statusNode = document.getElementById("storage-status");

    try {
        setStatusMessage(statusNode, busyMessage);
        const summary = await performStorageBulkAction(action);
        await loadAndRenderServerStorage();
        setStatusMessage(
            statusNode,
            buildStorageActionMessage(action, summary),
            summary.errors.length ? "error" : "success"
        );
    } catch (error) {
        console.error(`Failed to ${action} storage captures`, error);
        setStatusMessage(statusNode, error.message || "Serverový krok se nepovedl.", "error");
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

    const statusNode = document.getElementById("storage-status");
    const grid = document.getElementById("storage-grid");
    const filterForm = document.getElementById("storage-filter-form");
    const dateInput = document.getElementById("storage-date-input");
    const clearButton = document.getElementById("storage-date-clear");
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
            setStatusMessage(statusNode, "Načítám archiv...");
            try {
                await loadAndRenderServerStorage();
                setStatusMessage(statusNode, "");
            } catch (error) {
                console.error("Failed to switch storage status filter", error);
                setStatusMessage(statusNode, "Archiv se nepodařilo načíst.", "error");
            }
        });
    });

    filterForm.addEventListener("submit", async (event) => {
        event.preventDefault();
        storageState.capturedOn = dateInput.value || "";
        storageState.page = 1;

        try {
            setStatusMessage(statusNode, "Hledám podle data...");
            await loadAndRenderServerStorage();
            setStatusMessage(statusNode, "");
        } catch (error) {
            console.error("Failed to filter server archive by date", error);
            setStatusMessage(statusNode, "Vyhledávání podle data se nepovedlo.", "error");
        }
    });

    clearButton.addEventListener("click", async () => {
        dateInput.value = "";
        storageState.capturedOn = "";
        storageState.page = 1;

        try {
            setStatusMessage(statusNode, "Načítám celý archiv...");
            await loadAndRenderServerStorage();
            setStatusMessage(statusNode, "");
        } catch (error) {
            console.error("Failed to clear server archive date filter", error);
            setStatusMessage(statusNode, "Archiv se nepodařilo načíst.", "error");
        }
    });

    selectAll.addEventListener("change", handleStorageSelectAll);
    grid.addEventListener("change", handleStorageGridChange);

    prevButton.addEventListener("click", async () => {
        if (storageState.page <= 1) return;
        storageState.page -= 1;
        try {
            setStatusMessage(statusNode, "Načítám předchozí stránku...");
            await loadAndRenderServerStorage();
            setStatusMessage(statusNode, "");
        } catch (error) {
            console.error("Failed to load previous server archive page", error);
            setStatusMessage(statusNode, "Předchozí stránku se nepodařilo načíst.", "error");
        }
    });

    nextButton.addEventListener("click", async () => {
        if (storageState.totalPages === 0 || storageState.page >= storageState.totalPages) return;
        storageState.page += 1;
        try {
            setStatusMessage(statusNode, "Načítám další stránku...");
            await loadAndRenderServerStorage();
            setStatusMessage(statusNode, "");
        } catch (error) {
            console.error("Failed to load next server archive page", error);
            setStatusMessage(statusNode, "Další stránku se nepodařilo načíst.", "error");
        }
    });

    publishButton.addEventListener("click", async () => {
        await runStorageAction("publish", "Předávám vybrané fotografie ke kontrole před zveřejněním...");
    });

    unpublishButton.addEventListener("click", async () => {
        await runStorageAction("unpublish", "Stahuji vybrané fotografie z webu...");
    });

    deleteButton.addEventListener("click", async () => {
        await runStorageAction("delete", "Mažu vybrané fotografie ze serveru...");
    });

    window.addEventListener("beforeunload", clearStorageRefreshTimer);

    try {
        setStatusMessage(statusNode, "Načítám archiv...");
        await loadAndRenderServerStorage();
        setStatusMessage(statusNode, "");
    } catch (error) {
        console.error("Failed to initialize server storage page", error);
        setStatusMessage(statusNode, "Archiv se nepodařilo načíst.", "error");
    }
}

document.addEventListener("DOMContentLoaded", initServerStoragePage);
