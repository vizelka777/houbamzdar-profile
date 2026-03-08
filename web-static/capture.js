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
                store.createIndex("serverCaptureId", "serverCaptureId");
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

async function patchCaptureLocal(id, patch) {
    const items = await getAllCaptures();
    const target = items.find((item) => item.id === id);
    if (!target) return;

    await putCapture({ ...target, ...patch });
}

async function clearRemoteReference(serverCaptureId) {
    if (!serverCaptureId) return;

    const items = await getAllCaptures();
    const target = items.find((item) => item.serverCaptureId === serverCaptureId);
    if (!target) return;

    await putCapture({
        ...target,
        queued: false,
        serverCaptureId: "",
        uploadedAt: "",
        serverStatus: ""
    });
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

        const badges = [];
        if (item.queued) {
            badges.push('<span class="status-badge verified">Označeno pro server</span>');
        }
        if (item.serverCaptureId) {
            badges.push(`<span class="status-badge verified">${escapeHtml(item.serverStatus === "published" ? "Na serveru a zveřejněno" : "Na serveru")}</span>`);
        }

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
                ${badges.join("")}
            </div>
        `;

        grid.appendChild(card);
    });
}

function renderRemoteCaptures(captures) {
    const grid = document.getElementById("remote-capture-grid");
    if (!grid) return;

    grid.innerHTML = "";

    if (!captures.length) {
        const emptyState = document.createElement("div");
        emptyState.className = "capture-empty";
        emptyState.textContent = "Na serveru zatím není žádný uložený nález.";
        grid.appendChild(emptyState);
        return;
    }

    captures.forEach((capture) => {
        const card = document.createElement("article");
        card.className = "capture-item";

        const publicLink = capture.public_url
            ? `<a href="${escapeHtml(capture.public_url)}" target="_blank" rel="noreferrer" class="capture-link">Otevřít veřejnou verzi</a>`
            : "";
        const actionLabel = capture.status === "published" ? "Stáhnout z veřejného webu" : "Zveřejnit";
        const actionName = capture.status === "published" ? "unpublish" : "publish";

        card.innerHTML = `
            <div class="capture-meta">
                <h3>${escapeHtml(capture.original_file_name || "Nález")}</h3>
                <p>${escapeHtml(formatDateTime(capture.captured_at))}</p>
                <p>${escapeHtml(formatCoords(capture.latitude, capture.longitude))}</p>
                <p>${escapeHtml(`${Math.round((capture.size_bytes || 0) / 1024)} KB`)}</p>
                <span class="status-badge ${capture.status === "published" ? "verified" : "unverified"}">
                    ${escapeHtml(capture.status === "published" ? "Veřejné" : "Soukromé")}
                </span>
                ${publicLink}
            </div>
            <div class="capture-actions">
                <button type="button" class="btn btn-secondary capture-remote-action" data-action="${actionName}" data-capture-id="${escapeHtml(capture.id)}">
                    ${escapeHtml(actionLabel)}
                </button>
                <button type="button" class="btn btn-secondary capture-remote-action" data-action="delete" data-capture-id="${escapeHtml(capture.id)}">
                    Smazat ze serveru
                </button>
            </div>
        `;

        grid.appendChild(card);
    });
}

async function fetchRemoteCaptures() {
    const result = await apiGet("/api/captures");
    if (!result || !result.ok) {
        return [];
    }
    return result.captures || [];
}

async function refreshCaptureVault() {
    const [localItems, remoteItems] = await Promise.all([getAllCaptures(), fetchRemoteCaptures()]);
    renderCaptureStats(localItems);
    renderCaptureGrid(localItems);
    renderRemoteCaptures(remoteItems);
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
            serverCaptureId: "",
            uploadedAt: "",
            serverStatus: "",
            blob: file
        };

        await putCapture(record);
    }

    await refreshCaptureVault();
    setStatusMessage(document.getElementById("capture-status"), "Snímky jsou uložené v zařízení.", "success");
}

async function uploadCaptureToServer(capture) {
    const formData = new FormData();
    formData.append("photo", capture.blob, capture.fileName || "capture.jpg");
    formData.append("client_local_id", capture.id);
    formData.append("captured_at", capture.capturedAt);

    if (typeof capture.latitude === "number") {
        formData.append("latitude", String(capture.latitude));
    }
    if (typeof capture.longitude === "number") {
        formData.append("longitude", String(capture.longitude));
    }
    if (typeof capture.accuracy === "number") {
        formData.append("accuracy_meters", String(capture.accuracy));
    }

    const response = await fetch(`${API_URL}/api/captures`, {
        method: "POST",
        credentials: "include",
        body: formData
    });

    if (!response.ok) {
        const text = await response.text();
        throw new Error(text || `Upload failed (${response.status})`);
    }

    return response.json();
}

async function uploadQueuedCaptures() {
    const items = await getAllCaptures();
    const queuedItems = items.filter((item) => item.queued && !item.serverCaptureId);

    if (!queuedItems.length) {
        throw new Error("Nejdřív označte snímky, které chcete nahrát na server.");
    }

    for (const capture of queuedItems) {
        const result = await uploadCaptureToServer(capture);
        const remoteCapture = result.capture;
        await patchCaptureLocal(capture.id, {
            queued: false,
            serverCaptureId: remoteCapture.id,
            uploadedAt: remoteCapture.uploaded_at,
            serverStatus: remoteCapture.status
        });
    }
}

async function apiPostCaptureAction(captureID, action) {
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

async function apiDeleteCapture(captureID) {
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

async function handleRemoteAction(event) {
    const button = event.target.closest(".capture-remote-action");
    if (!button) return;

    const captureID = button.dataset.captureId;
    const action = button.dataset.action;
    const statusNode = document.getElementById("capture-status");

    try {
        if (action === "publish") {
            setStatusMessage(statusNode, "Zveřejňuji snímek...");
            await apiPostCaptureAction(captureID, "publish");
        } else if (action === "unpublish") {
            setStatusMessage(statusNode, "Stahuji snímek z veřejného webu...");
            await apiPostCaptureAction(captureID, "unpublish");
        } else if (action === "delete") {
            setStatusMessage(statusNode, "Mažu snímek ze serveru...");
            await apiDeleteCapture(captureID);
            await clearRemoteReference(captureID);
        }

        await refreshCaptureVault();
        setStatusMessage(statusNode, "Serverový stav byl aktualizován.", "success");
    } catch (error) {
        console.error("Failed to update remote capture", error);
        setStatusMessage(statusNode, "Serverový krok se nepovedl.", "error");
    }
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
    const uploadButton = document.getElementById("capture-upload-btn");
    const deleteButton = document.getElementById("capture-delete-btn");
    const statusNode = document.getElementById("capture-status");
    const remoteGrid = document.getElementById("remote-capture-grid");

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
            setStatusMessage(statusNode, "Vyberte snímky, které chcete připravit pro server.", "error");
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

    uploadButton.addEventListener("click", async () => {
        try {
            setStatusMessage(statusNode, "Nahrávám označené snímky do soukromého úložiště...");
            await uploadQueuedCaptures();
            await refreshCaptureVault();
            setStatusMessage(statusNode, "Vybrané snímky jsou uložené na serveru.", "success");
        } catch (error) {
            console.error("Failed to upload captures", error);
            setStatusMessage(statusNode, error.message || "Nahrání se nepovedlo.", "error");
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

    remoteGrid.addEventListener("click", handleRemoteAction);
}

document.addEventListener("DOMContentLoaded", initCapturePage);
