const PHOTO_DB_NAME = "hzd-photo-vault";
const PHOTO_STORE_NAME = "captures";
const GPS_TARGET_ACCURACY_METERS = 100;
const GPS_REQUEST_OPTIONS = {
    enableHighAccuracy: true,
    timeout: 12000,
    maximumAge: 0
};

let captureObjectUrls = [];
const captureGpsState = {
    watchId: null,
    checking: false,
    ready: false,
    latestPosition: null,
    bestPosition: null,
    lastError: null,
    pendingFiles: [],
    flushingPendingFiles: false
};

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

function formatAccuracyMeters(value) {
    const accuracy = Number(value);
    if (!Number.isFinite(accuracy)) {
        return "";
    }

    if (accuracy >= 1000) {
        return `${(accuracy / 1000).toFixed(1)} km`;
    }

    return `${Math.round(accuracy)} m`;
}

function getPositionAccuracyMeters(position) {
    const accuracy = Number(position?.coords?.accuracy);
    return Number.isFinite(accuracy) ? accuracy : null;
}

function isPositionAccurateEnough(position) {
    const accuracy = getPositionAccuracyMeters(position);
    return accuracy !== null && accuracy <= GPS_TARGET_ACCURACY_METERS;
}

function chooseBetterPosition(left, right) {
    if (!left) return right || null;
    if (!right) return left;

    const leftAccuracy = getPositionAccuracyMeters(left);
    const rightAccuracy = getPositionAccuracyMeters(right);

    if (leftAccuracy === null && rightAccuracy === null) {
        return right;
    }
    if (leftAccuracy === null) {
        return right;
    }
    if (rightAccuracy === null) {
        return left;
    }

    return rightAccuracy <= leftAccuracy ? right : left;
}

function renderCaptureGpsStatus(options = {}) {
    const node = document.getElementById("capture-gps-status");
    if (!node) return;

    const {
        kind = "",
        title = "",
        body = "",
        steps = [],
        note = ""
    } = options;

    if (!title && !body && !steps.length && !note) {
        node.hidden = true;
        node.className = "capture-gps-status";
        node.innerHTML = "";
        return;
    }

    const stepsHtml = steps.length
        ? `<ul class="capture-gps-status-list">${steps.map((item) => `<li>${escapeHtml(item)}</li>`).join("")}</ul>`
        : "";

    node.hidden = false;
    node.className = "capture-gps-status";
    if (kind) {
        node.classList.add(`is-${kind}`);
    }
    node.innerHTML = `
        ${title ? `<p class="capture-gps-status-title"><strong>${escapeHtml(title)}</strong></p>` : ""}
        ${body ? `<p class="capture-gps-status-copy">${escapeHtml(body)}</p>` : ""}
        ${stepsHtml}
        ${note ? `<p class="capture-gps-status-note">${escapeHtml(note)}</p>` : ""}
    `;
}

function getCaptureGpsCurrentPosition() {
    return captureGpsState.latestPosition || captureGpsState.bestPosition || null;
}

function getCaptureGpsAdviceItems(mode = "waiting") {
    if (mode === "permission") {
        return [
            "Povolte tomuto webu přístup k poloze a v telefonu zapněte přesnou polohu / GPS.",
            "Pokud jste oprávnění právě změnili, klepněte na „Zkusit GPS znovu“.",
            "Na iPhonu zkontrolujte Safari > Poloha > Přesná poloha, na Androidu oprávnění Poloha pro prohlížeč."
        ];
    }

    if (mode === "unsupported") {
        return [
            "Otevřete stránku v moderním mobilním prohlížeči, který geolokaci podporuje.",
            "Když má fotka GPS už uložené v EXIF, použijeme tyto souřadnice automaticky.",
            "Pro GPS z prohlížeče zkuste aktuální Chrome, Safari nebo Firefox v telefonu."
        ];
    }

    return [
        "Vyjděte ven nebo blíž k oknu a počkejte 10 až 30 sekund, než telefon chytí satelity.",
        "Zkontrolujte, že je v telefonu zapnutá přesná poloha / GPS.",
        "Nechte zapnuté Wi-Fi nebo mobilní data. Telefon si jimi často pomáhá při prvním určení polohy.",
        "Otáčení telefonem pomáhá hlavně kompasu, ne samotné přesnosti GPS."
    ];
}

function buildCaptureGpsStatusSummary() {
    const position = getCaptureGpsCurrentPosition();
    const accuracy = getPositionAccuracyMeters(position);
    const accuracyLabel = accuracy !== null ? formatAccuracyMeters(accuracy) : null;
    const error = captureGpsState.lastError;

    if (!navigator.geolocation) {
        return {
            kind: "error",
            title: "GPS není v tomto prohlížeči dostupné.",
            body: "Aktuální přesnost není k dispozici. Fotoaparát zůstane zamčený, dokud stránku neotevřete v prohlížeči s geolokací.",
            steps: getCaptureGpsAdviceItems("unsupported"),
            note: "Pokud fotka obsahuje GPS v EXIF, použije se tato poloha automaticky."
        };
    }

    if (captureGpsState.ready && position) {
        return {
            kind: "success",
            title: "GPS splňuje požadovanou přesnost.",
            body: `Aktuální přesnost je asi ${accuracyLabel}. To je v limitu ${GPS_TARGET_ACCURACY_METERS} m, fotoaparát je odemčený.`,
            note: "I při splněném limitu tuto hodnotu ukazujeme, abyste hned viděli, s jakou přesností se bude nová poloha používat."
        };
    }

    if (position && accuracy !== null) {
        return {
            kind: "warning",
            title: "GPS je zapnuté, ale ještě čekáme na lepší přesnost.",
            body: `Aktuální přesnost je asi ${accuracyLabel}. Potřebujeme nejvýš ${GPS_TARGET_ACCURACY_METERS} m, fotoaparát proto zatím zůstává zamčený.`,
            steps: getCaptureGpsAdviceItems("waiting"),
            note: "Jakmile telefon spadne na 100 m nebo lepší, fotoaparát se odemkne bez dalšího kroku."
        };
    }

    if (error && error.code === 1) {
        return {
            kind: "error",
            title: "GPS není povolené.",
            body: "Aktuální přesnost není k dispozici. Bez přístupu k poloze fotoaparát neodemkneme.",
            steps: getCaptureGpsAdviceItems("permission"),
            note: "Po povolení polohy klepněte na „Zkusit GPS znovu“."
        };
    }

    if (error && error.code === 2) {
        return {
            kind: "warning",
            title: "GPS zatím nedává použitelnou polohu.",
            body: "Aktuální přesnost ještě není k dispozici. Zkontrolujte, že je v telefonu zapnutá poloha, a chvíli počkejte na první fix.",
            steps: getCaptureGpsAdviceItems("waiting")
        };
    }

    if (error && error.code === 3) {
        return {
            kind: "warning",
            title: "Čekám na první přesnější GPS fix.",
            body: "Aktuální přesnost ještě není k dispozici. Jakmile telefon pošle polohu, začneme ukazovat její odchylku v metrech.",
            steps: getCaptureGpsAdviceItems("waiting")
        };
    }

    if (captureGpsState.checking) {
        return {
            kind: "pending",
            title: "Zjišťuji GPS polohu telefonu…",
            body: `Fotoaparát odemkneme až ve chvíli, kdy bude aktuální přesnost ${GPS_TARGET_ACCURACY_METERS} m nebo lepší.`
        };
    }

    return {
        kind: "pending",
        title: "Připravuji kontrolu GPS.",
        body: `Jakmile dostaneme první polohu, zobrazíme její aktuální přesnost a budeme čekat na limit ${GPS_TARGET_ACCURACY_METERS} m.`
    };
}

function updateCaptureLaunchControls() {
    const openCameraButton = document.getElementById("capture-open-camera");
    const refreshGpsButton = document.getElementById("capture-refresh-gps");
    const stateNode = document.getElementById("capture-open-camera-state");
    const position = getCaptureGpsCurrentPosition();
    const accuracy = getPositionAccuracyMeters(position);
    const accuracyLabel = accuracy !== null ? formatAccuracyMeters(accuracy) : null;

    if (openCameraButton) {
        openCameraButton.disabled = !captureGpsState.ready;
    }

    if (refreshGpsButton) {
        refreshGpsButton.disabled = false;
    }

    if (!stateNode) {
        return;
    }

    if (captureGpsState.ready && accuracyLabel) {
        stateNode.textContent = `GPS ${accuracyLabel}. Limit ${GPS_TARGET_ACCURACY_METERS} m splněn.`;
        return;
    }

    if (accuracyLabel) {
        stateNode.textContent = `Aktuální přesnost ${accuracyLabel}. Čekám na ${GPS_TARGET_ACCURACY_METERS} m nebo lepší.`;
        return;
    }

    if (!navigator.geolocation) {
        stateNode.textContent = "Tento prohlížeč neumí geolokaci.";
        return;
    }

    if (captureGpsState.lastError && captureGpsState.lastError.code === 1) {
        stateNode.textContent = "Povolte přístup k poloze a zkuste GPS znovu.";
        return;
    }

    stateNode.textContent = `Čekám na GPS do ${GPS_TARGET_ACCURACY_METERS} m.`;
}

function syncCaptureGpsUi() {
    renderCaptureGpsStatus(buildCaptureGpsStatusSummary());
    updateCaptureLaunchControls();
}

function stopCaptureGpsWatch() {
    if (captureGpsState.watchId !== null && navigator.geolocation) {
        navigator.geolocation.clearWatch(captureGpsState.watchId);
    }
    captureGpsState.watchId = null;
    captureGpsState.checking = false;
}

async function flushPendingCaptureFiles() {
    if (!captureGpsState.ready || captureGpsState.flushingPendingFiles || !captureGpsState.pendingFiles.length) {
        return;
    }

    const files = captureGpsState.pendingFiles.splice(0, captureGpsState.pendingFiles.length);
    captureGpsState.flushingPendingFiles = true;

    try {
        await handleCaptureSelection(files);
    } catch (error) {
        console.error("Failed to process queued capture files", error);
        setStatusMessage(document.getElementById("capture-status"), error.message || "Fotky se nepodařilo zpracovat.", "error");
    } finally {
        captureGpsState.flushingPendingFiles = false;
        if (captureGpsState.ready && captureGpsState.pendingFiles.length) {
            void flushPendingCaptureFiles();
        }
    }
}

function enqueueCaptureFilesForGpsGate(files) {
    const selectedFiles = Array.from(files || []).filter(Boolean);
    if (!selectedFiles.length) {
        return;
    }

    captureGpsState.pendingFiles.push(...selectedFiles);

    if (captureGpsState.ready) {
        void flushPendingCaptureFiles();
        return;
    }

    const accuracy = getPositionAccuracyMeters(getCaptureGpsCurrentPosition());
    const accuracyLine = accuracy !== null
        ? `Aktuální přesnost je asi ${formatAccuracyMeters(accuracy)}.`
        : "Aktuální přesnost ještě není k dispozici.";
    setStatusMessage(
        document.getElementById("capture-status"),
        `Snímky čekají na GPS. ${accuracyLine} Fotoaparát i zpracování pustíme až při ${GPS_TARGET_ACCURACY_METERS} m nebo lepší.`
    );
}

function handleCaptureGpsSuccess(position) {
    captureGpsState.checking = false;
    captureGpsState.lastError = null;
    captureGpsState.latestPosition = position;
    captureGpsState.bestPosition = chooseBetterPosition(captureGpsState.bestPosition, position);

    if (isPositionAccurateEnough(position) || isPositionAccurateEnough(captureGpsState.bestPosition)) {
        captureGpsState.ready = true;
        stopCaptureGpsWatch();
    }

    syncCaptureGpsUi();
    if (captureGpsState.ready) {
        void flushPendingCaptureFiles();
    }
}

function handleCaptureGpsError(error) {
    captureGpsState.checking = false;
    captureGpsState.lastError = error || null;

    if (error && error.code === 1) {
        stopCaptureGpsWatch();
    }

    syncCaptureGpsUi();
}

function startCaptureGpsWatch() {
    if (captureGpsState.ready) {
        syncCaptureGpsUi();
        return;
    }

    if (!navigator.geolocation) {
        captureGpsState.lastError = { code: "unsupported" };
        syncCaptureGpsUi();
        return;
    }

    if (captureGpsState.watchId !== null) {
        syncCaptureGpsUi();
        return;
    }

    captureGpsState.checking = true;
    captureGpsState.lastError = null;
    syncCaptureGpsUi();
    captureGpsState.watchId = navigator.geolocation.watchPosition(
        handleCaptureGpsSuccess,
        handleCaptureGpsError,
        GPS_REQUEST_OPTIONS
    );
}

function restartCaptureGpsWatch() {
    stopCaptureGpsWatch();
    captureGpsState.checking = false;
    captureGpsState.ready = false;
    captureGpsState.latestPosition = null;
    captureGpsState.bestPosition = null;
    captureGpsState.lastError = null;
    syncCaptureGpsUi();
    startCaptureGpsWatch();
}

function getReadyCapturePosition() {
    if (!captureGpsState.ready) {
        return null;
    }

    return chooseBetterPosition(captureGpsState.bestPosition, captureGpsState.latestPosition);
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
        const sendLabel = "Odeslat";
        const actionsHtml = uploadEnabled
            ? `
                    <button type="button" class="btn btn-primary btn-send-single capture-local-action" data-id="${escapeHtml(item.id)}" style="background: var(--success-color, #4CAF50); border-color: var(--success-color, #4CAF50);">${sendLabel}</button>
                    <button type="button" class="btn btn-danger btn-delete-single capture-local-action" data-id="${escapeHtml(item.id)}">Smazat</button>
                `
            : `
                    <button type="button" class="btn btn-danger btn-delete-single capture-local-action capture-local-action-single" data-id="${escapeHtml(item.id)}">Smazat</button>
                `;

        card.innerHTML = `
            <img src="${previewUrl}" alt="Nález hub" loading="lazy" style="width: 100%; aspect-ratio: 1; object-fit: cover; border-radius: var(--radius-sm);">
            <div>
                <p style="font-size: 0.85rem; color: var(--text-muted); margin: 0 0 0.25rem 0;">${dateStr}</p>
                <p style="font-size: 0.85rem; color: var(--text-muted); margin: 0 0 0.75rem 0;">${gpsIcon}${coordsStr}</p>
                <div class="capture-local-actions">
                    ${actionsHtml}
                </div>
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

async function handleCaptureSelection(files) {
    if (!files.length) return;

    const statusNode = document.getElementById("capture-status");
    const position = getReadyCapturePosition();

    if (!position) {
        enqueueCaptureFilesForGpsGate(files);
        return;
    }

    const readyAccuracy = getPositionAccuracyMeters(position);
    const readyAccuracyLabel = readyAccuracy !== null ? formatAccuracyMeters(readyAccuracy) : null;
    setStatusMessage(
        statusNode,
        readyAccuracyLabel
            ? `Zpracovávám snímky. Aktuální přesnost GPS je asi ${readyAccuracyLabel}.`
            : "Zpracovávám snímky s ověřenou GPS polohou."
    );
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
    const openCameraButton = document.getElementById("capture-open-camera");
    const refreshGpsButton = document.getElementById("capture-refresh-gps");
    const cameraInput = document.getElementById("capture-camera-input");
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

    syncCaptureGpsUi();
    startCaptureGpsWatch();

    if (openCameraButton && cameraInput) {
        openCameraButton.addEventListener("click", () => {
            if (!captureGpsState.ready) {
                syncCaptureGpsUi();
                return;
            }
            cameraInput.click();
        });

        cameraInput.addEventListener("change", async (event) => {
            const input = event.currentTarget;
            const selectedFiles = Array.from(input?.files || []);
            if (!selectedFiles.length) {
                return;
            }

            try {
                await handleCaptureSelection(selectedFiles);
            } catch (error) {
                console.error("Failed to process direct camera selection", error);
                setStatusMessage(statusNode, error.message || "Fotky se nepodařilo zpracovat.", "error");
            } finally {
                if (input) {
                    input.value = "";
                }
            }
        });
    }

    if (refreshGpsButton) {
        refreshGpsButton.addEventListener("click", () => {
            setStatusMessage(statusNode, "Spouštím novou kontrolu GPS...");
            restartCaptureGpsWatch();
        });
    }

    await refreshCaptureVault();

    if (typeof consumePendingCameraFiles === "function") {
        try {
            const pendingFiles = await consumePendingCameraFiles();
            if (pendingFiles.length) {
                enqueueCaptureFilesForGpsGate(pendingFiles);
            }
            if (pendingFiles.length || directCameraRequested) {
                window.history.replaceState({}, "", "/capture.html");
            }
        } catch (error) {
            console.error("Failed to process pending camera files", error);
            setStatusMessage(statusNode, "Fotky z rychlé kamery se nepodařilo načíst.", "error");
        }
    } else if (directCameraRequested) {
        window.history.replaceState({}, "", "/capture.html");
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
