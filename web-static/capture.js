const PHOTO_DB_NAME = "hzd-photo-vault";
const PHOTO_STORE_NAME = "captures";

let captureObjectUrls = [];

function captureUploadEnabled() {
    return Boolean(window.appSession && window.appSession.logged_in);
}

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

function isHeicLikeFile(file) {
    const fileName = (file?.name || "").toLowerCase();
    const mimeType = (file?.type || "").toLowerCase();

    return (
        mimeType === "image/heic" ||
        mimeType === "image/heif" ||
        mimeType === "image/heic-sequence" ||
        mimeType === "image/heif-sequence" ||
        fileName.endsWith(".heic") ||
        fileName.endsWith(".heif")
    );
}

function replaceFileExtension(fileName, extension) {
    if (!fileName) return `capture${extension}`;
    const index = fileName.lastIndexOf(".");
    if (index === -1) {
        return `${fileName}${extension}`;
    }
    return `${fileName.slice(0, index)}${extension}`;
}

function loadImageElementFromBlob(blob) {
    return new Promise((resolve, reject) => {
        const objectUrl = URL.createObjectURL(blob);
        const image = new Image();

        image.onload = () => {
            URL.revokeObjectURL(objectUrl);
            resolve(image);
        };
        image.onerror = () => {
            URL.revokeObjectURL(objectUrl);
            reject(new Error("Browser failed to decode image"));
        };
        image.src = objectUrl;
    });
}

async function normalizeSelectedFile(file) {
    let sourceBlob = file;

    // Pokud je to HEIC, převedeme ho nejprve pomocí heic2any
    if (isHeicLikeFile(file) && typeof heic2any === "function") {
        try {
            const result = await heic2any({
                blob: file,
                toType: "image/jpeg",
                quality: 0.9
            });
            sourceBlob = Array.isArray(result) ? result[0] : result;
        } catch (e) {
            console.warn("heic2any failed", e);
        }
    }

    // Načteme obrázek do elementu Image. Moderní prohlížeče zde automaticky aplikují EXIF rotaci.
    const image = await loadImageElementFromBlob(sourceBlob);
    
    // Omezíme maximální rozměry pro rychlejší upload a úsporu místa
    const MAX_WIDTH = 1920;
    const MAX_HEIGHT = 1920;
    
    let width = image.naturalWidth || image.width;
    let height = image.naturalHeight || image.height;
    
    if (width > MAX_WIDTH || height > MAX_HEIGHT) {
        const ratio = Math.min(MAX_WIDTH / width, MAX_HEIGHT / height);
        width = Math.round(width * ratio);
        height = Math.round(height * ratio);
    }

    const canvas = document.createElement("canvas");
    canvas.width = width;
    canvas.height = height;
    
    const context = canvas.getContext("2d");
    if (!context) {
        throw new Error("Canvas is not available");
    }
    
    // Vykreslíme na canvas, čímž zafixujeme pixely ve správné orientaci a odstraníme EXIF metadata
    context.drawImage(image, 0, 0, width, height);

    const convertedBlob = await new Promise((resolve, reject) => {
        canvas.toBlob((blob) => {
            if (!blob) {
                reject(new Error("Failed to process image via canvas"));
                return;
            }
            resolve(blob);
        }, "image/jpeg", 0.85); // 85% kvalita
    });

    return {
        blob: convertedBlob,
        fileName: replaceFileExtension(file.name || `nalez-${Date.now()}.jpg`, ".jpg"),
        mimeType: "image/jpeg"
    };
}

function getSelectedCaptureIds() {
    return Array.from(document.querySelectorAll(".capture-checkbox:checked")).map((checkbox) => checkbox.value);
}

function releaseCaptureObjectUrls() {
    captureObjectUrls.forEach((url) => URL.revokeObjectURL(url));
    captureObjectUrls = [];
}

function renderCaptureStats(items) {
    const totalNode = document.getElementById("capture-total");
    if (totalNode) {
        totalNode.textContent = String(items.length);
    }
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
        card.style.display = "flex";
        card.style.flexDirection = "column";
        card.style.gap = "0.5rem";
        card.style.background = "var(--surface)";
        card.style.padding = "0.5rem";
        card.style.borderRadius = "var(--radius-md)";
        card.style.boxShadow = "var(--shadow-soft)";

        const previewUrl = URL.createObjectURL(item.blob);
        captureObjectUrls.push(previewUrl);

        const dateStr = escapeHtml(formatDateTime(item.capturedAt));
        const coordsStr = escapeHtml(formatCoords(item.latitude, item.longitude));
        const gpsIcon = `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="vertical-align: text-bottom; margin-right: 4px;"><path d="M21 10c0 7-9 13-9 13s-9-6-9-13a9 9 0 0 1 18 0z"></path><circle cx="12" cy="10" r="3"></circle></svg>`;
        const uploadEnabled = captureUploadEnabled();
        const sendLabel = uploadEnabled ? "Odeslat" : "Přihlásit se pro nahrání";
        const sendDisabled = uploadEnabled ? "" : "disabled";
        const sendTitle = uploadEnabled ? "" : 'title="Snímek zůstane uložený v telefonu. Pro nahrání na server se přihlaste později."';
        const offlineNote = uploadEnabled
            ? ""
            : '<p style="font-size: 0.8rem; color: var(--text-muted); margin: 0.5rem 0 0 0;">Snímek zůstane uložený v telefonu a nahrajete ho až po přihlášení.</p>';

        card.innerHTML = `
            <img src="${previewUrl}" alt="Nález hub" loading="lazy" style="width: 100%; aspect-ratio: 1; object-fit: cover; border-radius: var(--radius-sm);">
            <div>
                <p style="font-size: 0.85rem; color: var(--text-muted); margin: 0 0 0.25rem 0;">${dateStr}</p>
                <p style="font-size: 0.85rem; color: var(--text-muted); margin: 0 0 0.75rem 0;">${gpsIcon}${coordsStr}</p>
                <div style="display: flex; gap: 0.5rem;">
                    <button type="button" class="btn btn-primary btn-send-single" data-id="${escapeHtml(item.id)}" style="flex: 1; background: var(--success-color, #4CAF50); border-color: var(--success-color, #4CAF50);" ${sendDisabled} ${sendTitle}>${sendLabel}</button>
                    <button type="button" class="btn btn-danger btn-delete-single" data-id="${escapeHtml(item.id)}" style="flex: 1;">Smazat</button>
                </div>
                ${offlineNote}
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
        const privatePreview = `${API_URL}/api/captures/${encodeURIComponent(capture.id)}/preview`;
        const previewHtml = buildCaptureImageTag(capture, {
            variant: "thumb",
            alt: "Náhled nahrané fotografie",
            className: "capture-thumb",
            loading: "lazy",
            sizes: "(max-width: 720px) 100vw, 320px"
        }) || `<img src="${escapeHtml(privatePreview)}" alt="Náhled nahrané fotografie" class="capture-thumb" loading="lazy">`;
        const actionLabel = capture.status === "published" ? "Zrušit publikaci" : "Publikovat";
        const actionName = capture.status === "published" ? "unpublish" : "publish";

        card.innerHTML = `
            ${previewHtml}
            <div class="capture-meta">
                <h3>${escapeHtml(capture.original_file_name || "Nález")}</h3>
                <p>${escapeHtml(formatDateTime(capture.captured_at))}</p>
                <p>${escapeHtml(formatCoords(capture.latitude, capture.longitude))}</p>
                <p>${escapeHtml(`${Math.round((capture.size_bytes || 0) / 1024)} KB`)}</p>
                <span class="status-badge ${capture.status === "published" ? "verified" : "unverified"}">
                    ${escapeHtml(capture.status === "published" ? "Publikované" : "Nepublikované")}
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
    const localItems = await getAllCaptures();
    renderCaptureStats(localItems);
    renderCaptureGrid(localItems);
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

    setStatusMessage(document.getElementById("capture-status"), "Zpracovávám snímky a zjišťuji polohu...");
    const position = await readCurrentPosition();
    let storedCount = 0;
    let convertedCount = 0;
    const failedFiles = [];

    for (const file of files) {
        try {
            // Zkusit získat GPS z EXIF přes exifr předtím, než canvas EXIF zničí
            let exifLat = null;
            let exifLon = null;
            if (typeof exifr !== 'undefined') {
                try {
                    const gpsData = await exifr.gps(file);
                    if (gpsData && gpsData.latitude != null && gpsData.longitude != null) {
                        exifLat = gpsData.latitude;
                        exifLon = gpsData.longitude;
                    }
                } catch (exifErr) {
                    console.warn("Nepodařilo se vyčíst EXIF GPS z", file.name, exifErr);
                }
            }

            const normalized = await normalizeSelectedFile(file);
            if (normalized.mimeType === "image/jpeg" && isHeicLikeFile(file)) {
                convertedCount += 1;
            }

            const finalLat = exifLat !== null ? exifLat : (position ? position.coords.latitude : null);
            const finalLon = exifLon !== null ? exifLon : (position ? position.coords.longitude : null);
            // Pokud jsme použili EXIF, neznáme přesnost, dáme null. Pokud lokaci z browseru, dáme accuracy.
            const finalAcc = (exifLat !== null) ? null : (position ? position.coords.accuracy : null);

        const record = {
            id: typeof crypto !== "undefined" && crypto.randomUUID ? crypto.randomUUID() : `${Date.now()}-${Math.random()}`,
            fileName: normalized.fileName,
            mimeType: normalized.mimeType,
            size: normalized.blob.size || 0,
            capturedAt: new Date().toISOString(),
            latitude: finalLat,
            longitude: finalLon,
            accuracy: finalAcc,
            queued: false,
            serverCaptureId: "",
            uploadedAt: "",
            serverStatus: "",
            blob: normalized.blob
        };

        await putCapture(record);
            storedCount += 1;
        } catch (error) {
            console.error("Failed to normalize selected file", error);
            failedFiles.push(file.name || "snímek");
        }
    }

    await refreshCaptureVault();
    const statusNode = document.getElementById("capture-status");
    if (storedCount === 0) {
        throw new Error("Nepodařilo se uložit žádný snímek. HEIC/HEIF zkuste na iPhonu přepnout na Most Compatible.");
    }

    let message = "Snímky jsou uložené v zařízení.";
    if (convertedCount > 0) {
        message = `${message} ${convertedCount} souborů HEIC/HEIF bylo převedeno do JPEG.`;
    }
    if (failedFiles.length > 0) {
        message = `${message} ${failedFiles.length} souborů se nepodařilo zpracovat.`;
    }
    setStatusMessage(statusNode, message, "success");
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
        if (result.capture?.id) {
            await deleteCaptures([capture.id]);
        }
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
            setStatusMessage(statusNode, "Publikuji snímek...");
            await apiPostCaptureAction(captureID, "publish");
        } else if (action === "unpublish") {
            setStatusMessage(statusNode, "Ruším publikaci snímku...");
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
    const me = session && session.logged_in ? await apiGet("/api/me") : null;
    setAppIdentity(session, me);
    renderHeader(session, me);

    const statusNode = document.getElementById("capture-status");
    const gridNode = document.getElementById("capture-grid");
    const footerLink = document.getElementById("capture-footer-link");
    const directCameraRequested = new URLSearchParams(window.location.search).get("source") === "camera";
    if (!indexedDbAvailable()) {
        setStatusMessage(statusNode, "Tento prohlížeč neumí IndexedDB. Zkuste moderní mobilní prohlížeč.", "error");
        return;
    }

    if (!captureUploadEnabled()) {
        setStatusMessage(statusNode, "Fotky se teď ukládají jen do telefonu. Na server je nahrajete později po přihlášení.");
        if (footerLink) {
            footerLink.href = `${API_URL}/auth/login`;
            footerLink.textContent = "Přihlásit se pro nahrání fotek";
        }
    } else if (footerLink) {
        footerLink.href = "/server-storage.html";
        footerLink.textContent = "Přejít k nahraným fotkám";
    }

    await refreshCaptureVault();

    if (typeof consumePendingCameraFiles === "function") {
        try {
            const pendingFiles = await consumePendingCameraFiles();
            if (pendingFiles.length) {
                await handleCaptureSelection(pendingFiles);
            }
            if (pendingFiles.length || directCameraRequested) {
                window.history.replaceState({}, "", "/capture.html");
            }
        } catch (error) {
            console.error("Failed to process pending camera files", error);
            setStatusMessage(statusNode, "Fotky z rychlé kamery se nepodařilo načíst.", "error");
        }
    }

    if (gridNode) {
        gridNode.addEventListener("click", async (event) => {
            const sendBtn = event.target.closest(".btn-send-single");
            if (sendBtn) {
                if (!captureUploadEnabled()) {
                    setStatusMessage(statusNode, "Snímek zůstává uložený v telefonu. Pro nahrání na server se přihlaste později.");
                    return;
                }
                const id = sendBtn.dataset.id;
                try {
                    setStatusMessage(statusNode, "Nahrávám snímek na server...");
                    const items = await getAllCaptures();
                    const target = items.find(i => i.id === id);
                    if (!target) throw new Error("Snímek nebyl nalezen.");
                    
                    const result = await uploadCaptureToServer(target);
                    if (result.capture?.id) {
                        await deleteCaptures([id]);
                        await refreshCaptureVault();
                        setStatusMessage(statusNode, "Snímek byl úspěšně nahrán.", "success");
                    }
                } catch (error) {
                    console.error("Upload failed", error);
                    setStatusMessage(statusNode, error.message || "Nahrání se nepovedlo.", "error");
                }
                return;
            }

            const deleteBtn = event.target.closest(".btn-delete-single");
            if (deleteBtn) {
                const id = deleteBtn.dataset.id;
                if (window.confirm("Opravdu chcete tento snímek smazat?")) {
                    try {
                        await deleteCaptures([id]);
                        await refreshCaptureVault();
                        setStatusMessage(statusNode, "Snímek byl smazán.", "success");
                    } catch (error) {
                        console.error("Delete failed", error);
                        setStatusMessage(statusNode, "Snímky se nepodařilo smazat.", "error");
                    }
                }
                return;
            }
        });
    }

}

document.addEventListener("DOMContentLoaded", initCapturePage);
