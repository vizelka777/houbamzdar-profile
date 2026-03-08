const PHOTO_DB_NAME = "hzd-photo-vault";
const PHOTO_STORE_NAME = "captures";

let captureObjectUrls = [];

function indexedDbAvailable() {
    return typeof window !== "undefined" && typeof window.indexedDB !== "undefined";
}

function openPhotoVault() {
    return new Promise((resolve, reject) => {
        if (!indexedDbAvailable()) {
            reject(new Error("IndexedDB is not available"));
            return;
        }

        const request = window.indexedDB.open(PHOTO_DB_NAME, 1);

        request.onupgradeneeded = () => {
            const db = request.result;
            if (!db.objectStoreNames.contains(PHOTO_STORE_NAME)) {
                const store = db.createObjectStore(PHOTO_STORE_NAME, { keyPath: "id" });
                store.createIndex("capturedAt", "capturedAt");
                store.createIndex("queued", "queued");
            }
        };

        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error || new Error("Failed to open photo vault"));
    });
}

function txDone(tx) {
    return new Promise((resolve, reject) => {
        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error || new Error("IndexedDB transaction failed"));
        tx.onabort = () => reject(tx.error || new Error("IndexedDB transaction aborted"));
    });
}

function reqDone(request) {
    return new Promise((resolve, reject) => {
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error || new Error("IndexedDB request failed"));
    });
}

async function getAllCaptures() {
    const db = await openPhotoVault();
    const tx = db.transaction(PHOTO_STORE_NAME, "readonly");
    const request = tx.objectStore(PHOTO_STORE_NAME).getAll();
    const items = await reqDone(request);
    await txDone(tx);
    return items.sort((left, right) => right.capturedAt.localeCompare(left.capturedAt));
}

async function putCapture(capture) {
    const db = await openPhotoVault();
    const tx = db.transaction(PHOTO_STORE_NAME, "readwrite");
    tx.objectStore(PHOTO_STORE_NAME).put(capture);
    await txDone(tx);
}

async function updateQueuedState(ids, queued) {
    if (!ids.length) return;
    const selectedIds = new Set(ids);
    const items = await getAllCaptures();
    const db = await openPhotoVault();
    const tx = db.transaction(PHOTO_STORE_NAME, "readwrite");
    const store = tx.objectStore(PHOTO_STORE_NAME);

    items
        .filter((item) => selectedIds.has(item.id))
        .forEach((item) => store.put({ ...item, queued }));

    await txDone(tx);
}

async function deleteCaptures(ids) {
    if (!ids.length) return;
    const db = await openPhotoVault();
    const tx = db.transaction(PHOTO_STORE_NAME, "readwrite");
    const store = tx.objectStore(PHOTO_STORE_NAME);

    ids.forEach((id) => store.delete(id));

    await txDone(tx);
}

function formatCoords(lat, lng) {
    if (typeof lat !== "number" || typeof lng !== "number") {
        return "Bez GPS";
    }

    return `${lat.toFixed(5)}, ${lng.toFixed(5)}`;
}

function getSelectedCaptureIds() {
    return Array.from(document.querySelectorAll(".capture-checkbox:checked")).map((checkbox) => checkbox.value);
}

function releaseCaptureObjectUrls() {
    captureObjectUrls.forEach((url) => URL.revokeObjectURL(url));
    captureObjectUrls = [];
}

function renderCaptureStats(items) {
    setText("capture-total", String(items.length));
    setText("capture-queued", String(items.filter((item) => item.queued).length));

    const latestWithCoords = items.find((item) => typeof item.latitude === "number" && typeof item.longitude === "number");
    setText("capture-location", latestWithCoords ? formatCoords(latestWithCoords.latitude, latestWithCoords.longitude) : "Bez GPS");
}

function renderCaptureGrid(items) {
    const grid = document.getElementById("capture-grid");
    if (!grid) return;

    releaseCaptureObjectUrls();
    grid.innerHTML = "";

    if (!items.length) {
        const emptyState = document.createElement("div");
        emptyState.className = "capture-empty";
        emptyState.textContent = "Zatím tu nejsou žádné nálezy. Otevřete fotoaparát a uložte první snímek.";
        grid.appendChild(emptyState);
        return;
    }

    items.forEach((item) => {
        const card = document.createElement("article");
        card.className = "capture-item";

        const previewUrl = URL.createObjectURL(item.blob);
        captureObjectUrls.push(previewUrl);

        const queuedBadge = item.queued ? `<span class="status-badge verified">Připraveno k nahrání</span>` : "";
        card.innerHTML = `
            <label class="capture-select">
                <input class="capture-checkbox" type="checkbox" value="${escapeHtml(item.id)}">
                <span>Vybrat</span>
            </label>
            <img src="${previewUrl}" alt="Nález hub" class="capture-thumb">
            <div class="capture-meta">
                <h3>${escapeHtml(item.fileName || "Nález")}</h3>
                <p>${escapeHtml(formatDateTime(item.capturedAt))}</p>
                <p>${escapeHtml(formatCoords(item.latitude, item.longitude))}</p>
                <p>${escapeHtml(`${Math.round((item.size || 0) / 1024)} KB`)}</p>
                ${queuedBadge}
            </div>
        `;

        grid.appendChild(card);
    });
}

async function refreshCaptureVault() {
    const items = await getAllCaptures();
    renderCaptureStats(items);
    renderCaptureGrid(items);
}

function readCurrentPosition() {
    return new Promise((resolve) => {
        if (!navigator.geolocation) {
            resolve(null);
            return;
        }

        navigator.geolocation.getCurrentPosition(
            (position) => resolve(position),
            () => resolve(null),
            { enableHighAccuracy: true, timeout: 12000, maximumAge: 0 }
        );
    });
}

async function handleCaptureSelection(files) {
    if (!files.length) return;

    setStatusMessage(document.getElementById("capture-status"), "Získávám polohu a ukládám snímky...");
    const position = await readCurrentPosition();

    for (const file of files) {
        const record = {
            id: typeof crypto !== "undefined" && crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`,
            fileName: file.name || `nalez-${Date.now()}.jpg`,
            mimeType: file.type || "image/jpeg",
            size: file.size || 0,
            capturedAt: new Date().toISOString(),
            latitude: position ? position.coords.latitude : null,
            longitude: position ? position.coords.longitude : null,
            accuracy: position ? position.coords.accuracy : null,
            queued: false,
            blob: file
        };

        await putCapture(record);
    }

    await refreshCaptureVault();
    setStatusMessage(document.getElementById("capture-status"), "Snímky jsou uložené v zařízení.", "success");
}

async function initCapturePage() {
    if (document.body.dataset.page !== "capture") return;

    const session = await apiGet("/api/session");
    if (!session || !session.logged_in) {
        window.location.href = "/";
        return;
    }

    const me = await apiGet("/api/me");
    if (!me) return;

    renderHeader(session, me);

    const fileInput = document.getElementById("capture-file-input");
    const queueButton = document.getElementById("capture-queue-btn");
    const deleteButton = document.getElementById("capture-delete-btn");
    const statusNode = document.getElementById("capture-status");

    if (!indexedDbAvailable()) {
        setStatusMessage(statusNode, "Tento prohlížeč neumí IndexedDB. Zkuste moderní mobilní prohlížeč.", "error");
        return;
    }

    await refreshCaptureVault();

    fileInput.addEventListener("change", async (event) => {
        const selectedFiles = Array.from(event.target.files || []);
        try {
            await handleCaptureSelection(selectedFiles);
        } catch (error) {
            console.error("Failed to save captures", error);
            setStatusMessage(statusNode, "Fotky se nepodařilo uložit do zařízení.", "error");
        } finally {
            fileInput.value = "";
        }
    });

    queueButton.addEventListener("click", async () => {
        const ids = getSelectedCaptureIds();
        if (!ids.length) {
            setStatusMessage(statusNode, "Vyberte snímky, které chcete připravit k nahrání.", "error");
            return;
        }

        try {
            await updateQueuedState(ids, true);
            await refreshCaptureVault();
            setStatusMessage(statusNode, "Vybrané snímky jsou připravené k nahrání na server.", "success");
        } catch (error) {
            console.error("Failed to queue captures", error);
            setStatusMessage(statusNode, "Snímky se nepodařilo označit.", "error");
        }
    });

    deleteButton.addEventListener("click", async () => {
        const ids = getSelectedCaptureIds();
        if (!ids.length) {
            setStatusMessage(statusNode, "Vyberte snímky, které chcete smazat.", "error");
            return;
        }

        try {
            await deleteCaptures(ids);
            await refreshCaptureVault();
            setStatusMessage(statusNode, "Vybrané snímky byly odstraněny ze zařízení.", "success");
        } catch (error) {
            console.error("Failed to delete captures", error);
            setStatusMessage(statusNode, "Snímky se nepodařilo smazat.", "error");
        }
    });
}

document.addEventListener("DOMContentLoaded", initCapturePage);
