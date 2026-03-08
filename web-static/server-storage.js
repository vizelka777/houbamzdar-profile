const SERVER_STORAGE_PAGE_SIZE = 12;

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

function updateStorageActionState() {
    const selectedCount = storageState.selectedIds.size;
    const publishButton = document.getElementById("storage-publish-btn");
    const unpublishButton = document.getElementById("storage-unpublish-btn");
    const deleteButton = document.getElementById("storage-delete-btn");
    const selectedCountNode = document.getElementById("storage-selected-count");

    if (selectedCountNode) {
        selectedCountNode.textContent = `Vybráno: ${selectedCount}`;
    }

    if (publishButton) {
        publishButton.disabled = selectedCount === 0 || storageState.status !== "private";
    }
    if (unpublishButton) {
        unpublishButton.disabled = selectedCount === 0 || storageState.status !== "published";
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
        return;
    }

    storageState.captures.forEach((capture) => {
        const card = document.createElement("article");
        card.className = "capture-item";

        const previewUrl = `${API_URL}/api/captures/${encodeURIComponent(capture.id)}/preview`;
        const publicLink = capture.public_url
            ? `<a href="${escapeHtml(capture.public_url)}" target="_blank" rel="noreferrer" class="capture-link">Otevřít veřejnou verzi</a>`
            : "";

        card.innerHTML = `
            <div class="capture-item-head">
                <label class="capture-select">
                    <input class="storage-capture-checkbox" type="checkbox" value="${escapeHtml(capture.id)}">
                    <span>Vybrat</span>
                </label>
                <span class="status-badge ${capture.status === "published" ? "verified" : "unverified"}">
                    ${escapeHtml(capture.status === "published" ? "Veřejné" : "Soukromé")}
                </span>
            </div>
            <img src="${escapeHtml(previewUrl)}" alt="Soukromý náhled nálezu" class="capture-thumb" loading="lazy">
            <div class="capture-meta">
                <h3>${escapeHtml(capture.original_file_name || "Nález")}</h3>
                <p>${escapeHtml(formatDateTime(capture.captured_at))}</p>
                <p>${escapeHtml(formatStorageCoords(capture.latitude, capture.longitude))}</p>
                <p>${escapeHtml(`${Math.round((capture.size_bytes || 0) / 1024)} KB`)}</p>
                ${publicLink}
            </div>
        `;

        grid.appendChild(card);
    });

    updateStorageActionState();
    renderStorageSummary();
}

async function loadAndRenderServerStorage() {
    await fetchServerStoragePage();
    syncStorageToggleButtons();
    renderServerStorageGrid();
}

async function apiPostStorageAction(captureID, action) {
    const response = await fetch(`${API_URL}/api/captures/${encodeURIComponent(captureID)}/${action}`, {
        method: "POST",
        credentials: "include"
    });

    if (!response.ok) {
        const text = await response.text();
        throw new Error(text || `Action ${action} failed`);
    }

    return response.json();
}

async function apiDeleteStorageCapture(captureID) {
    const response = await fetch(`${API_URL}/api/captures/${encodeURIComponent(captureID)}`, {
        method: "DELETE",
        credentials: "include"
    });

    if (!response.ok) {
        const text = await response.text();
        throw new Error(text || "Delete failed");
    }

    return response.json();
}

async function performStorageBulkAction(action) {
    const selected = selectedStorageCaptures();
    if (!selected.length) {
        throw new Error("Nejdřív vyberte alespoň jednu fotografii.");
    }

    let applicable = selected;
    if (action === "publish") {
        applicable = selected.filter((capture) => capture.status === "private");
    } else if (action === "unpublish") {
        applicable = selected.filter((capture) => capture.status === "published");
    }

    if (!applicable.length) {
        throw new Error(
            action === "publish"
                ? "Vybrané fotky už nejsou soukromé."
                : action === "unpublish"
                    ? "Vybrané fotky už nejsou veřejné."
                    : "Vybrané fotky nejde zpracovat."
        );
    }

    for (const capture of applicable) {
        if (action === "delete") {
            await apiDeleteStorageCapture(capture.id);
            continue;
        }
        await apiPostStorageAction(capture.id, action);
    }
}

function handleStorageGridChange(event) {
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

async function runStorageAction(action, busyMessage, doneMessage) {
    const statusNode = document.getElementById("storage-status");

    try {
        setStatusMessage(statusNode, busyMessage);
        await performStorageBulkAction(action);
        await loadAndRenderServerStorage();
        setStatusMessage(statusNode, doneMessage, "success");
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
        await runStorageAction("publish", "Zveřejňuji vybrané fotografie...", "Vybrané fotografie jsou zveřejněné.");
    });

    unpublishButton.addEventListener("click", async () => {
        await runStorageAction("unpublish", "Stahuji vybrané fotografie z webu...", "Vybrané fotografie jsou zase soukromé.");
    });

    deleteButton.addEventListener("click", async () => {
        await runStorageAction("delete", "Mažu vybrané fotografie ze serveru...", "Vybrané fotografie byly ze serveru smazané.");
    });

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
