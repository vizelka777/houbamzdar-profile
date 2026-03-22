const API_URL = "https://api.houbamzdar.cz";
const DEFAULT_AVATAR_URL = "/default-avatar.png";
const PROFILE_LAST_VISIT_KEY = "hzd_last_profile_visit_at";
const PHOTO_INTAKE_DB_NAME = "hzd-photo-intake";
const PHOTO_INTAKE_STORE_NAME = "pending-files";

function photoIntakeAvailable() {
    return typeof window !== "undefined" && typeof window.indexedDB !== "undefined";
}

function openPhotoIntakeDb() {
    return new Promise((resolve, reject) => {
        if (!photoIntakeAvailable()) {
            reject(new Error("IndexedDB is not available"));
            return;
        }

        const request = window.indexedDB.open(PHOTO_INTAKE_DB_NAME, 1);
        request.onupgradeneeded = () => {
            const db = request.result;
            if (!db.objectStoreNames.contains(PHOTO_INTAKE_STORE_NAME)) {
                const store = db.createObjectStore(PHOTO_INTAKE_STORE_NAME, { keyPath: "id" });
                store.createIndex("queuedAt", "queuedAt");
            }
        };
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error || new Error("Failed to open photo intake database"));
    });
}

function photoIntakeTxDone(tx) {
    return new Promise((resolve, reject) => {
        tx.oncomplete = () => resolve();
        tx.onerror = () => reject(tx.error || new Error("IndexedDB transaction failed"));
        tx.onabort = () => reject(tx.error || new Error("IndexedDB transaction aborted"));
    });
}

function photoIntakeReqDone(request) {
    return new Promise((resolve, reject) => {
        request.onsuccess = () => resolve(request.result);
        request.onerror = () => reject(request.error || new Error("IndexedDB request failed"));
    });
}

async function queuePendingCameraFiles(files) {
    if (!photoIntakeAvailable()) {
        return false;
    }

    const db = await openPhotoIntakeDb();
    const tx = db.transaction(PHOTO_INTAKE_STORE_NAME, "readwrite");
    const store = tx.objectStore(PHOTO_INTAKE_STORE_NAME);
    await photoIntakeReqDone(store.clear());

    const timestamp = Date.now();
    files.forEach((file, index) => {
        store.put({
            id: typeof crypto !== "undefined" && crypto.randomUUID ? crypto.randomUUID() : `${timestamp}-${index}-${Math.random()}`,
            file,
            queuedAt: timestamp + index
        });
    });

    await photoIntakeTxDone(tx);
    return true;
}

async function consumePendingCameraFiles() {
    if (!photoIntakeAvailable()) {
        return [];
    }

    const db = await openPhotoIntakeDb();
    const tx = db.transaction(PHOTO_INTAKE_STORE_NAME, "readwrite");
    const store = tx.objectStore(PHOTO_INTAKE_STORE_NAME);
    const items = await photoIntakeReqDone(store.getAll());
    store.clear();
    await photoIntakeTxDone(tx);

    return (items || [])
        .sort((left, right) => Number(left.queuedAt || 0) - Number(right.queuedAt || 0))
        .map((item) => item.file)
        .filter(Boolean);
}

function escapeHtml(unsafe) {
    if (!unsafe) return "";
    return unsafe
        .toString()
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;")
        .replace(/'/g, "&#039;");
}

async function apiGet(path) {
    try {
        const res = await fetch(`${API_URL}${path}`, {
            method: "GET",
            credentials: "include"
        });
        if (!res.ok) throw new Error(`HTTP error ${res.status}`);
        return await res.json();
    } catch (e) {
        console.error("API GET Error", e);
        return null;
    }
}

async function apiPost(path, body = null) {
    try {
        const options = {
            method: "POST",
            credentials: "include",
            headers: {}
        };

        if (body) {
            options.headers["Content-Type"] = "application/json";
            options.body = JSON.stringify(body);
        }

        const res = await fetch(`${API_URL}${path}`, options);
        if (!res.ok) throw new Error(`HTTP error ${res.status}`);
        return await res.json();
    } catch (e) {
        console.error("API POST Error", e);
        return null;
    }
}

async function apiPut(path, body = null) {
    try {
        const options = {
            method: "PUT",
            credentials: "include",
            headers: {}
        };

        if (body) {
            options.headers["Content-Type"] = "application/json";
            options.body = JSON.stringify(body);
        }

        const res = await fetch(`${API_URL}${path}`, options);
        if (!res.ok) throw new Error(`HTTP error ${res.status}`);
        return await res.json();
    } catch (e) {
        console.error("API PUT Error", e);
        return null;
    }
}

async function apiDelete(path) {
    try {
        const res = await fetch(`${API_URL}${path}`, {
            method: "DELETE",
            credentials: "include"
        });
        if (!res.ok) throw new Error(`HTTP error ${res.status}`);
        return await res.json();
    } catch (e) {
        console.error("API DELETE Error", e);
        return null;
    }
}

async function apiJsonRequest(path, options = {}) {
    const requestOptions = {
        method: options.method || "GET",
        credentials: "include",
        headers: {}
    };

    if (options.body !== undefined) {
        requestOptions.headers["Content-Type"] = "application/json";
        requestOptions.body = JSON.stringify(options.body);
    }

    const response = await fetch(`${API_URL}${path}`, requestOptions);
    const payload = await response.json().catch(() => null);
    if (!response.ok) {
        throw new Error(payload?.error || payload?.message || `HTTP error ${response.status}`);
    }
    return payload;
}

function createLinkButton(label, href, className) {
    const link = document.createElement("a");
    link.className = `btn ${className}`;
    link.href = href;
    link.textContent = label;
    return link;
}

function createIconLinkButton(href, label, iconSVG, className) {
    const link = document.createElement("a");
    link.className = `btn ${className} btn-icon`;
    link.href = href;
    link.setAttribute("aria-label", label);
    link.setAttribute("title", label);
    link.innerHTML = `
        <span class="btn-icon-glyph" aria-hidden="true">${iconSVG}</span>
        <span class="sr-only">${escapeHtml(label)}</span>
    `;
    return link;
}

function createIconActionButton(label, iconSVG, className, handler) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `btn ${className} btn-icon`;
    button.setAttribute("aria-label", label);
    button.setAttribute("title", label);
    button.innerHTML = `
        <span class="btn-icon-glyph" aria-hidden="true">${iconSVG}</span>
        <span class="sr-only">${escapeHtml(label)}</span>
    `;
    button.addEventListener("click", handler);
    return button;
}

function createLabeledIconLinkButton(href, label, iconSVG, className) {
    const link = document.createElement("a");
    link.className = `btn ${className} btn-icon btn-icon-labeled`;
    link.href = href;
    link.setAttribute("aria-label", label);
    link.innerHTML = `
        <span class="btn-icon-glyph" aria-hidden="true">${iconSVG}</span>
        <span class="btn-icon-label">${escapeHtml(label)}</span>
    `;
    return link;
}

async function handleDirectCameraSelection(event) {
    const input = event.currentTarget;
    const selectedFiles = Array.from(input?.files || []);

    if (!selectedFiles.length) {
        return;
    }

    try {
        await queuePendingCameraFiles(selectedFiles);
    } catch (error) {
        console.error("Failed to queue direct camera files", error);
    } finally {
        if (input) {
            input.value = "";
        }
    }

    window.location.href = "/capture.html?source=camera";
}

function createDirectCameraButton(label, iconSVG, className) {
    const wrapper = document.createElement("label");
    wrapper.className = `btn ${className} btn-icon`;
    wrapper.setAttribute("aria-label", label);
    wrapper.setAttribute("title", label);
    wrapper.innerHTML = `
        <span class="btn-icon-glyph" aria-hidden="true">${iconSVG}</span>
        <span class="sr-only">${escapeHtml(label)}</span>
    `;

    const input = document.createElement("input");
    input.type = "file";
    input.accept = "image/*";
    input.capture = "environment";
    input.multiple = true;
    input.className = "sr-only";
    input.addEventListener("change", handleDirectCameraSelection);
    wrapper.appendChild(input);

    return wrapper;
}

function buildHeaderMenuIconMarkup(icon) {
    if (!icon) {
        return "";
    }

    return `<span class="header-menu-icon" aria-hidden="true">${icon}</span>`;
}

function closeOpenHeaderMenusExcept(activeMenu = null) {
    document.querySelectorAll(".header-menu[open]").forEach((menu) => {
        if (activeMenu && menu === activeMenu) {
            return;
        }
        menu.removeAttribute("open");
    });
}

function eventPointHitsElement(event, element) {
    if (!event || !element || typeof event.clientX !== "number" || typeof event.clientY !== "number") {
        return false;
    }
    const rect = element.getBoundingClientRect();
    return (
        event.clientX >= rect.left &&
        event.clientX <= rect.right &&
        event.clientY >= rect.top &&
        event.clientY <= rect.bottom
    );
}

function resolveActiveHeaderMenuPointerTarget(event) {
    const menu = event?.target?.closest?.(".header-menu") || null;
    if (!menu) {
        return null;
    }

    const panel = menu.querySelector(".header-menu-panel");
    if (panel?.contains(event.target)) {
        return menu;
    }

    const summary = menu.querySelector("summary");
    if (summary?.contains(event.target) && eventPointHitsElement(event, summary)) {
        return menu;
    }

    return null;
}

function resolveActiveHeaderMenuFocusTarget(target) {
    const interactiveArea = target?.closest?.(".header-menu-panel, .header-menu summary") || null;
    return interactiveArea ? interactiveArea.closest(".header-menu") : null;
}

function ensureHeaderMenuAutoClose() {
    if (window.__hzdHeaderMenuAutoCloseBound) {
        return;
    }

    window.__hzdHeaderMenuAutoCloseBound = true;

    document.addEventListener("pointerdown", (event) => {
        const activeMenu = resolveActiveHeaderMenuPointerTarget(event);
        closeOpenHeaderMenusExcept(activeMenu);
    }, true);

    document.addEventListener("focusin", (event) => {
        const activeMenu = resolveActiveHeaderMenuFocusTarget(event.target);
        closeOpenHeaderMenusExcept(activeMenu);
    });
}

function createHeaderMenuButton(label, iconSVG, className, items, options = {}) {
    const { hideLabel = false, lead = null } = options;
    ensureHeaderMenuAutoClose();
    const details = document.createElement("details");
    details.className = "header-menu";

    const summary = document.createElement("summary");
    summary.className = `btn ${className} btn-icon header-control-button${hideLabel ? "" : " btn-icon-labeled"}`;
    summary.setAttribute("aria-label", label);
    summary.innerHTML = `
        <span class="btn-icon-glyph" aria-hidden="true">${iconSVG}</span>
        ${hideLabel ? '' : `<span class="btn-icon-label">${escapeHtml(label)}</span>`}
    `;
    details.appendChild(summary);

    const panel = document.createElement("div");
    panel.className = "header-menu-panel";

    if (lead) {
        const leadNode = document.createElement("div");
        leadNode.className = "header-menu-lead";
        leadNode.innerHTML = `
            ${lead.eyebrow ? `<span class="section-label">${escapeHtml(lead.eyebrow)}</span>` : ""}
            ${lead.title ? `<strong>${escapeHtml(lead.title)}</strong>` : ""}
            ${lead.copy ? `<p>${escapeHtml(lead.copy)}</p>` : ""}
        `;
        panel.appendChild(leadNode);
    }

    items.forEach((item) => {
        if (item.type === "action" && typeof item.handler === "function") {
            const button = document.createElement("button");
            button.type = "button";
            button.className = `btn ${item.className || "btn-secondary"} header-menu-action`;
            button.innerHTML = `
                ${buildHeaderMenuIconMarkup(item.icon)}
                <span>${escapeHtml(item.label || "Akce")}</span>
            `;
            button.addEventListener("click", async () => {
                details.removeAttribute("open");
                await item.handler();
            });
            panel.appendChild(button);
            return;
        }

        if (item.type === "file-input" && typeof item.handler === "function") {
            const label = document.createElement("label");
            label.className = "header-menu-item";
            label.style.cursor = "pointer";
            label.innerHTML = `
                ${buildHeaderMenuIconMarkup(item.icon)}
                <span class="header-menu-item-copy">
                    <span>${escapeHtml(item.label)}</span>
                    ${item.note ? `<small class="header-menu-note">${escapeHtml(item.note)}</small>` : ""}
                </span>
            `;

            const input = document.createElement("input");
            input.type = "file";
            input.accept = "image/*";
            input.capture = "environment";
            input.multiple = true;
            input.className = "sr-only";
            input.addEventListener("change", (e) => {
                details.removeAttribute("open");
                item.handler(e);
            });
            label.appendChild(input);
            panel.appendChild(label);
            return;
        }

        const link = document.createElement("a");
        link.className = "header-menu-item";
        link.href = item.href;
        link.innerHTML = `
            ${buildHeaderMenuIconMarkup(item.icon)}
            <span class="header-menu-item-copy">
                <span>${escapeHtml(item.label)}</span>
                ${item.note ? `<small class="header-menu-note">${escapeHtml(item.note)}</small>` : ""}
            </span>
        `;
        link.addEventListener("click", () => {
            details.removeAttribute("open");
        });
        panel.appendChild(link);
    });

    details.appendChild(panel);

    return details;
}

function createActionButton(label, className, handler) {
    const button = document.createElement("button");
    button.type = "button";
    button.className = `btn ${className}`;
    button.textContent = label;
    button.addEventListener("click", handler);
    return button;
}

function setText(id, value) {
    const node = document.getElementById(id);
    if (!node) return;
    node.textContent = value;
}

function normalizeDateTimeInput(dateString) {
    const raw = String(dateString || "").trim();
    if (!raw || raw.startsWith("0001-01-01")) {
        return "";
    }

    const date = new Date(raw);
    if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) {
        return "";
    }

    return raw;
}

function hasMeaningfulDateTime(dateString) {
    return Boolean(normalizeDateTimeInput(dateString));
}

function formatDateTime(dateString, fallback = "Právě teď") {
    const normalized = normalizeDateTimeInput(dateString);
    if (!normalized) return fallback;
    const date = new Date(normalized);
    return new Intl.DateTimeFormat("cs-CZ", {
        dateStyle: "medium",
        timeStyle: "short"
    }).format(date);
}

function formatHoubickaCount(value) {
    const amount = Number(value) || 0;
    if (amount === 1) return "1 houbička";
    if (amount >= 2 && amount <= 4) return `${amount} houbičky`;
    return `${amount} houbiček`;
}

function buildPublicProfileURL(userID) {
    if (!userID) {
        return "/public-profile.html";
    }
    return `/public-profile.html?user=${encodeURIComponent(userID)}`;
}

const CAPTURE_IMAGE_VARIANTS = {
    original: null,
    thumb: {
        width: "384",
        quality: "68",
        widths: [192, 256, 320, 384, 512, 640, 768],
        sizes: "(max-width: 720px) 50vw, (max-width: 1200px) 33vw, 384px"
    },
    mapMarker: {
        width: "128",
        quality: "60",
        widths: [64, 96, 128, 160, 192, 256],
        sizes: "56px"
    },
    popup: {
        width: "640",
        quality: "72",
        widths: [320, 480, 640, 768, 960, 1280],
        sizes: "(max-width: 720px) 82vw, 320px"
    },
    lightbox: {
        width: "1600",
        quality: "80",
        widths: [640, 960, 1280, 1600, 2048],
        sizes: "100vw"
    }
};

function getCaptureImageVariantPreset(variant = "original") {
    return CAPTURE_IMAGE_VARIANTS[variant] || CAPTURE_IMAGE_VARIANTS.original;
}

function isOptimizerEligibleCaptureURL(url) {
    const hostname = String(url?.hostname || "").toLowerCase();
    return hostname === "foto.houbamzdar.cz" || hostname.endsWith(".b-cdn.net");
}

function applyCaptureImageVariant(urlString, variant = "original", overrides = {}) {
    const preset = getCaptureImageVariantPreset(variant);
    if (!preset) {
        return urlString;
    }

    try {
        const url = new URL(urlString, window.location.origin);
        if (!isOptimizerEligibleCaptureURL(url)) {
            return urlString;
        }

        const params = {
            ...preset,
            ...(overrides && typeof overrides === "object" ? overrides : {})
        };
        delete params.widths;
        delete params.sizes;

        Object.entries(params).forEach(([key, value]) => {
            if (value === null || value === undefined || value === "") {
                return;
            }
            url.searchParams.set(key, value);
        });
        return url.toString();
    } catch (error) {
        console.warn("Failed to build capture image variant", error);
        return urlString;
    }
}

function buildCaptureImageURL(capture, variant = "original") {
    if (!capture) return "";
    const baseURL = capture.image_url || capture.public_url || "";
    if (baseURL) {
        return applyCaptureImageVariant(baseURL, variant);
    }
    return `${API_URL}/api/captures/${encodeURIComponent(capture.id)}/preview`;
}

function buildCaptureImageSrcSet(capture, variant = "original") {
    const baseURL = capture?.image_url || capture?.public_url || "";
    if (!baseURL) {
        return "";
    }

    const preset = getCaptureImageVariantPreset(variant);
    const widths = Array.isArray(preset?.widths) ? preset.widths : [];
    if (!widths.length) {
        return "";
    }

    return [...new Set(widths.map((value) => Number(value)).filter((value) => value > 0))]
        .sort((left, right) => left - right)
        .map((width) => `${applyCaptureImageVariant(baseURL, variant, { width: String(width) })} ${width}w`)
        .join(", ");
}

function buildCaptureImageSizes(variant = "original", override = "") {
    const explicit = String(override || "").trim();
    if (explicit) {
        return explicit;
    }
    return String(getCaptureImageVariantPreset(variant)?.sizes || "").trim();
}

function buildCaptureImageTag(capture, {
    variant = "original",
    alt = "",
    className = "",
    loading = "lazy",
    sizes = "",
    fetchPriority = "",
    extraAttrs = {}
} = {}) {
    const src = buildCaptureImageURL(capture, variant);
    if (!src) {
        return "";
    }

    const attrs = [
        `src="${escapeHtml(src)}"`,
        `alt="${escapeHtml(alt)}"`,
        className ? `class="${escapeHtml(className)}"` : "",
        loading ? `loading="${escapeHtml(loading)}"` : "",
        'decoding="async"',
        fetchPriority ? `fetchpriority="${escapeHtml(fetchPriority)}"` : ""
    ];

    const srcset = buildCaptureImageSrcSet(capture, variant);
    const resolvedSizes = buildCaptureImageSizes(variant, sizes);
    if (srcset) {
        attrs.push(`srcset="${escapeHtml(srcset)}"`);
        if (resolvedSizes) {
            attrs.push(`sizes="${escapeHtml(resolvedSizes)}"`);
        }
    }

    Object.entries(extraAttrs || {}).forEach(([name, value]) => {
        if (!name || value === null || value === undefined || value === "") {
            return;
        }
        attrs.push(`${name}="${escapeHtml(value)}"`);
    });

    return `<img ${attrs.filter(Boolean).join(" ")}>`;
}

function setCaptureImageElement(img, capture, {
    variant = "original",
    alt = "",
    loading = "",
    sizes = "",
    fetchPriority = ""
} = {}) {
    if (!img) {
        return;
    }

    const src = buildCaptureImageURL(capture, variant);
    if (!src) {
        img.removeAttribute("src");
        img.removeAttribute("srcset");
        img.removeAttribute("sizes");
        img.removeAttribute("fetchpriority");
        img.alt = alt || "";
        return;
    }

    img.src = src;
    img.alt = alt || "";
    img.decoding = "async";

    if (loading) {
        img.loading = loading;
    }
    if (fetchPriority) {
        img.setAttribute("fetchpriority", fetchPriority);
    } else {
        img.removeAttribute("fetchpriority");
    }

    const srcset = buildCaptureImageSrcSet(capture, variant);
    if (srcset) {
        img.srcset = srcset;
        const resolvedSizes = buildCaptureImageSizes(variant, sizes);
        if (resolvedSizes) {
            img.sizes = resolvedSizes;
        } else {
            img.removeAttribute("sizes");
        }
    } else {
        img.removeAttribute("srcset");
        img.removeAttribute("sizes");
    }
}

function captureHasCoordinates(capture) {
    return Boolean(
        capture &&
        capture.latitude !== null &&
        capture.latitude !== undefined &&
        capture.longitude !== null &&
        capture.longitude !== undefined
    );
}

function buildCaptureMapData(capture) {
    if (!captureHasCoordinates(capture)) return null;
    return {
        lat: Number(capture.latitude),
        lon: Number(capture.longitude)
    };
}

function formatCaptureCoordinates(capture) {
    if (!captureHasCoordinates(capture)) {
        return "Souřadnice nejsou k dispozici.";
    }

    return `${Number(capture.latitude).toFixed(5)}, ${Number(capture.longitude).toFixed(5)}`;
}

function formatCaptureProbability(probability) {
    const numeric = Number(probability);
    if (!Number.isFinite(numeric) || numeric <= 0) {
        return "";
    }
    return `${Math.round(numeric * 100)} %`;
}

function buildCaptureSpeciesEntries(capture) {
    if (!capture) return [];

    const species = Array.isArray(capture.mushroom_species) ? capture.mushroom_species.filter(Boolean) : [];
    if (species.length > 0) {
        return species.map((item) => {
            const czechName = String(item.czech_official_name || "").trim();
            const latinName = String(item.latin_name || "").trim();
            const probability = formatCaptureProbability(item.probability);

            let label = "";
            if (czechName && latinName && czechName.toLowerCase() !== latinName.toLowerCase()) {
                label = `${czechName} (${latinName})`;
            } else {
                label = czechName || latinName;
            }
            if (!label) {
                return "";
            }
            if (!probability) {
                return label;
            }
            return `${label} • ${probability}`;
        }).filter(Boolean);
    }

    const fallback = buildCaptureSpeciesLabel(capture);
    return fallback ? [fallback] : [];
}

function buildCaptureSpeciesTooltip(capture) {
    const entries = buildCaptureSpeciesEntries(capture);
    if (entries.length === 0) {
        return "";
    }
    return entries.join("\n");
}

function buildCaptureSpeciesLabel(capture) {
    if (!capture) return "";

    const czechName = String(capture.mushroom_primary_czech_name || "").trim();
    const latinName = String(capture.mushroom_primary_latin_name || "").trim();
    const probability = formatCaptureProbability(capture.mushroom_primary_probability);

    let label = "";
    if (czechName && latinName && czechName.toLowerCase() !== latinName.toLowerCase()) {
        label = `${czechName} (${latinName})`;
    } else {
        label = czechName || latinName;
    }

    if (!label) {
        return "";
    }
    if (!probability) {
        return label;
    }
    return `${label} • ${probability}`;
}

function buildCaptureRegionLabel(capture) {
    if (!capture) return "";

    const parts = [];
    const krajName = String(capture.kraj_name || "").trim();
    const okresName = capture.coordinates_free ? String(capture.okres_name || "").trim() : "";
    const obecName = capture.coordinates_free ? String(capture.obec_name || "").trim() : "";

    if (obecName) {
        parts.push(obecName);
    }
    if (okresName && !parts.includes(okresName)) {
        parts.push(okresName);
    }
    if (krajName && !parts.includes(krajName)) {
        parts.push(krajName);
    }

    return parts.join(", ");
}

function buildCaptureKrajLabel(capture) {
    if (!capture) return "";
    return String(capture.kraj_name || "").trim();
}

function buildCaptureRegionSearchNote(capture) {
    if (!capture) return "";

    if (capture.coordinates_free) {
        if (String(capture.obec_name || "").trim()) {
            return "Vyhledatelné až po obec";
        }
        if (String(capture.okres_name || "").trim()) {
            return "Vyhledatelné až po okres";
        }
    }

    if (String(capture.kraj_name || "").trim()) {
        return "Vyhledatelné podle kraje";
    }

    return "";
}

function buildCaptureAccessBadgeHtml(capture) {
    if (!capture) return "";

    if (capture.coordinates_free && (captureHasCoordinates(capture) || capture.coordinates_locked)) {
        return '<span class="capture-access-badge capture-access-badge-free">Zdarma</span>';
    }

    if (capture.coordinates_locked) {
        return '<span class="capture-access-badge capture-access-badge-paid">1 houbička</span>';
    }

    if (captureHasCoordinates(capture)) {
        return '<span class="capture-access-badge capture-access-badge-map">Mapa</span>';
    }

    return "";
}

function setAppIdentity(session, me) {
    window.appSession = session || null;
    window.appMe = me || null;
}

function userCanModerateClient(user = window.appMe) {
    return Boolean(user && (user.is_moderator || user.is_admin));
}

function userCanAdminClient(user = window.appMe) {
    return Boolean(user && user.is_admin);
}

function refreshHoubickaBalanceViews() {
    const balance = window.appMe && typeof window.appMe.houbicka_balance === "number"
        ? window.appMe.houbicka_balance
        : 0;

    setText("metric-houbicky", formatHoubickaCount(balance));
    setText("houbicka-balance", formatHoubickaCount(balance));
}

function getPreviousProfileVisit() {
    try {
        const previousVisit = window.localStorage.getItem(PROFILE_LAST_VISIT_KEY);
        window.localStorage.setItem(PROFILE_LAST_VISIT_KEY, new Date().toISOString());
        return previousVisit ? formatDateTime(previousVisit) : "Právě teď";
    } catch (error) {
        console.error("Failed to read last visit", error);
        return "Právě teď";
    }
}

function buildProfileInsights(user) {
    const alerts = [];

    if (!user.preferred_username) {
        alerts.push("Nastavte si krátký veřejný nick, ať vás komunita snadno pozná.");
    }

    if (!user.email_verified) {
        alerts.push("Potvrďte e-mail v AHOJ420 pro důvěryhodnější profil.");
    }

    if (!user.phone_number_verified) {
        alerts.push("Potvrďte telefon v AHOJ420, ať se profil posune výš.");
    }

    if (!user.picture) {
        alerts.push("Přidejte profilovou fotku pro silnější důvěru.");
    }

    if (!user.about_me) {
        alerts.push("Doplňte krátké veřejné představení.");
    }

    const bonuses = [
        Boolean(user.preferred_username),
        Boolean(user.picture),
        Boolean(user.about_me),
        Boolean(user.email_verified),
        Boolean(user.phone_number_verified)
    ];

    const score =
        (user.preferred_username ? 20 : 0) +
        (user.email_verified ? 20 : 0) +
        (user.phone_number_verified ? 20 : 0) +
        (user.picture ? 20 : 0) +
        (user.about_me ? 20 : 0);

    let statusLabel = "Rozpracovaný";
    let trustLabel = "Buduje se";
    let tone = "is-low";

    if (score >= 85) {
        statusLabel = "Výborný";
        trustLabel = "Vysoká důvěra";
        tone = "is-good";
    } else if (score >= 60) {
        statusLabel = "Aktivní";
        trustLabel = "Stabilní důvěra";
        tone = "is-mid";
    }

    return {
        alerts,
        score,
        statusLabel,
        trustLabel,
        tone,
        bonusCount: bonuses.filter(Boolean).length,
        bonusTotal: bonuses.length
    };
}

function isNicknameFormatValid(value) {
    const nickname = String(value || "").trim();
    if (!nickname) {
        return false;
    }
    if (Array.from(nickname).length > 12) {
        return false;
    }
    return /^[\p{L}\p{N}]+$/u.test(nickname);
}

function renderNicknameSuggestions(suggestions, onPick) {
    const node = document.getElementById("nickname-suggestions");
    if (!node) {
        return;
    }

    const items = Array.isArray(suggestions)
        ? suggestions.filter((item) => typeof item === "string" && item.trim())
        : [];

    if (!items.length) {
        node.innerHTML = "";
        node.hidden = true;
        return;
    }

    node.hidden = false;
    node.innerHTML = items.map((suggestion) => `
        <button type="button" class="nickname-suggestion-btn" data-nickname-suggestion="${escapeHtml(suggestion)}">
            ${escapeHtml(suggestion)}
        </button>
    `).join("");

    node.querySelectorAll("[data-nickname-suggestion]").forEach((button) => {
        button.addEventListener("click", () => {
            if (typeof onPick === "function") {
                onPick(button.getAttribute("data-nickname-suggestion") || "");
            }
        });
    });
}

function applyPreferredUsernameUpdate(me, preferredUsername) {
    if (!me) {
        return;
    }

    me.preferred_username = preferredUsername;
    if (window.appMe) {
        window.appMe.preferred_username = preferredUsername;
    }
    if (window.appSession && window.appSession.user) {
        window.appSession.user.preferred_username = preferredUsername;
    }

    setText("account-name", preferredUsername || "Bez uživatelského jména");
    renderHeader(window.appSession, window.appMe || me);
}

function setNicknameEditorMode(isEditing, me) {
    const displayNode = document.getElementById("nickname-display");
    const displayValueNode = document.getElementById("nickname-display-value");
    const editorNode = document.getElementById("nickname-editor");
    const input = document.getElementById("nickname-input");
    const saveBtn = document.getElementById("save-nickname-btn");
    const editBtn = document.getElementById("edit-nickname-btn");

    if (!displayNode || !displayValueNode || !editorNode || !input || !saveBtn || !editBtn) {
        return;
    }

    const nickname = String(me?.preferred_username || "").trim();
    displayValueNode.textContent = nickname || "Bez uživatelského jména";
    displayNode.hidden = isEditing || !nickname;
    editorNode.hidden = !isEditing;
    saveBtn.hidden = !isEditing;
    editBtn.hidden = isEditing || !nickname;

    if (isEditing) {
        input.value = nickname;
    }
}

function setupNicknameEditor(me) {
    const input = document.getElementById("nickname-input");
    const saveBtn = document.getElementById("save-nickname-btn");
    const editBtn = document.getElementById("edit-nickname-btn");
    const statusNode = document.getElementById("nickname-status");

    if (!input || !saveBtn || !me) {
        return;
    }

    input.value = me.preferred_username || "";
    const hasExistingNickname = Boolean(String(me.preferred_username || "").trim());
    setNicknameEditorMode(!hasExistingNickname, me);

    const selectSuggestion = (suggestion) => {
        input.value = suggestion;
        input.focus();
        setStatusMessage(statusNode, "Návrh je vložený. Uložte ho tlačítkem.", "success");
    };

    if (editBtn) {
        editBtn.addEventListener("click", () => {
            setNicknameEditorMode(true, me);
            renderNicknameSuggestions([], null);
            setStatusMessage(statusNode, "");
            input.focus();
            input.select();
        });
    }

    saveBtn.addEventListener("click", async () => {
        const nextNickname = String(input.value || "").trim();
        if (!isNicknameFormatValid(nextNickname)) {
            renderNicknameSuggestions([], null);
            setStatusMessage(statusNode, "Nick musí mít 1 až 12 znaků a obsahovat jen písmena nebo číslice.", "error");
            return;
        }

        saveBtn.disabled = true;
        renderNicknameSuggestions([], null);
        setStatusMessage(statusNode, "Ukládám nick...");

        try {
            const response = await fetch(`${API_URL}/api/me/nickname`, {
                method: "POST",
                credentials: "include",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ preferred_username: nextNickname })
            });
            const payload = await response.json().catch(() => null);
            if (!response.ok) {
                renderNicknameSuggestions(payload?.preferred_suggestions || [], selectSuggestion);
                setStatusMessage(statusNode, payload?.message || "Nick se nepodařilo uložit.", "error");
                return;
            }

            applyPreferredUsernameUpdate(me, payload?.preferred_username || nextNickname);
            renderNicknameSuggestions([], null);
            setNicknameEditorMode(false, me);
            setStatusMessage(statusNode, "Nick byl uložen.", "success");
        } catch (error) {
            console.error("Failed to update nickname", error);
            renderNicknameSuggestions([], null);
            setNicknameEditorMode(true, me);
            setStatusMessage(statusNode, "Nick se nepodařilo uložit.", "error");
        } finally {
            saveBtn.disabled = false;
        }
    });
}

function renderProfilePicture(elementId, picture, altText) {
    const node = document.getElementById(elementId);
    if (!node) return;

    const imageUrl = picture || DEFAULT_AVATAR_URL;
    node.innerHTML = `<img src="${escapeHtml(imageUrl)}" alt="${escapeHtml(altText)}">`;
}

function renderSimpleList(elementId, items, emptyText) {
    const list = document.getElementById(elementId);
    if (!list) return;

    list.innerHTML = "";

    if (!items.length) {
        const placeholder = document.createElement("li");
        placeholder.className = "list-placeholder";
        placeholder.textContent = emptyText;
        list.appendChild(placeholder);
        return;
    }

    items.forEach((item) => {
        const li = document.createElement("li");
        li.textContent = item;
        list.appendChild(li);
    });
}

async function initSharedHeaderOnly() {
    const authButtons = document.getElementById("auth-buttons");
    if (!authButtons) {
        return;
    }

    const session = await apiGet("/api/session");
    let profile = null;
    if (session && session.logged_in) {
        profile = await apiGet("/api/me");
    }

    setAppIdentity(session, profile);
    renderHeader(session, profile);
}

function renderHeader(session, profile = null) {
    const authButtons = document.getElementById("auth-buttons");
    if (!authButtons) return;

    authButtons.innerHTML = "";
    const identity = profile || session?.user || null;

    const menuIcon = `
        <svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round">
            <line x1="3" y1="12" x2="21" y2="12"></line>
            <line x1="3" y1="6" x2="21" y2="6"></line>
            <line x1="3" y1="18" x2="21" y2="18"></line>
        </svg>
    `;
    const cameraIcon = `
        <span class="header-emoji-icon">📷</span>
    `;
    const createPostIcon = `
        <span class="header-emoji-icon">✍️</span>
    `;
    const logoutIcon = `
        <svg viewBox="0 0 24 24" aria-hidden="true" stroke="currentColor" stroke-width="2.5" fill="none" stroke-linecap="round" stroke-linejoin="round">
            <path d="M10 3H6a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h4"></path>
            <polyline points="14 7 19 12 14 17"></polyline>
            <line x1="19" y1="12" x2="9" y2="12"></line>
        </svg>
    `;

    const avatarUrl = identity?.picture;
    const profileIcon = avatarUrl
        ? `<img src="${escapeHtml(avatarUrl)}" alt="Avatar" loading="lazy">`
        : `
        <svg viewBox="0 0 24 24" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2.25" stroke-linecap="round" stroke-linejoin="round">
            <path d="M20 21a8 8 0 0 0-16 0"></path>
            <circle cx="12" cy="8" r="4"></circle>
        </svg>
    `;

    if (session && session.logged_in) {
        const menuItems = [
            { href: "/create-post.html", label: "Vytvořit publikaci", icon: "✍️" },
            { type: "file-input", label: "Vyfotit nový nález", handler: handleDirectCameraSelection, icon: "📷" },
            { href: "/capture.html", label: "Zpracování fotek", note: "lokální snímky, výběr a nahrání na server", icon: "🧺" },
            { href: "/server-storage.html", label: "Nahrané fotky", note: "to, co už je uložené v Bunny", icon: "🗂️" },
            { href: "/feed.html", label: "Zeď úlovků", icon: "📰" },
            { href: "/gallery.html", label: "Galerie", icon: "🖼️" },
            { href: "/map.html", label: "Mapa", icon: "🗺️" },
        ];

        if (userCanModerateClient(identity)) {
            menuItems.push({ href: "/moderation.html", label: "Moderace", icon: "🛡️" });
        }
        if (userCanAdminClient(identity)) {
            menuItems.push({ href: "/admin.html", label: "Administrace", icon: "⚙️" });
        }

        const menuProfileIcon = avatarUrl
            ? `<img src="${escapeHtml(avatarUrl)}" alt="Avatar" loading="lazy">`
            : "👤";

        menuItems.push({ href: "/me.html", label: "Můj profil", icon: menuProfileIcon });

        const cameraButton = createDirectCameraButton("Přidat úlovek", cameraIcon, "btn-secondary");
        cameraButton.classList.add("header-control-button");

        const createPostButton = createIconLinkButton("/create-post.html", "Vytvořit publikaci", createPostIcon, "btn-secondary");
        createPostButton.classList.add("header-control-button");

        const profileButton = createIconLinkButton("/me.html", "Můj profil", profileIcon, "btn-secondary");
        profileButton.classList.add("header-control-button");

        const logoutButton = createIconActionButton("Odhlásit", logoutIcon, "btn-secondary", logoutFlow);
        logoutButton.classList.add("header-control-button");

        const username = session.user?.preferred_username || identity?.preferred_username || "hoste";
        const menuButton = createHeaderMenuButton("Menu", menuIcon, "btn-secondary", [
            ...menuItems,
            { type: "action", label: "Odhlásit", className: "btn-danger", handler: logoutFlow, icon: "↪" }
        ], {
            hideLabel: true,
            lead: {
                eyebrow: "Menu",
                title: `Ahoj, ${username}`
            }
        });

        authButtons.appendChild(cameraButton);
        authButtons.appendChild(createPostButton);
        authButtons.appendChild(profileButton);
        authButtons.appendChild(logoutButton);
        authButtons.appendChild(menuButton);
        return;
    }

    const menuItems = [
        { href: "/feed.html", label: "Zeď úlovků", icon: "📰" },
        { href: "/gallery.html", label: "Galerie", icon: "🖼️" },
        { href: "/map.html", label: "Mapa", icon: "🗺️" },
    ];

    const loginIcon = `
        <svg viewBox="0 0 24 24" aria-hidden="true" stroke="currentColor" stroke-width="2.5" fill="none" stroke-linecap="round" stroke-linejoin="round">
            <path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4"></path>
            <polyline points="10 17 15 12 10 7"></polyline>
            <line x1="15" y1="12" x2="3" y2="12"></line>
        </svg>
    `;

    const loginButton = createIconLinkButton(`${API_URL}/auth/login`, "Přihlášení", loginIcon, "btn-primary");
    loginButton.classList.add("header-control-button");
    const cameraButton = createDirectCameraButton("Přidat úlovek", cameraIcon, "btn-secondary");
    cameraButton.classList.add("header-control-button");

    const menuButton = createHeaderMenuButton("Menu", menuIcon, "btn-secondary", menuItems, {
        hideLabel: true,
        lead: {
            eyebrow: "Veřejné menu",
            title: "Houbam Zdar",
            copy: "Galerie, mapa a zeď úlovků jsou dostupné i bez přihlášení."
        }
    });

    authButtons.appendChild(cameraButton);
    authButtons.appendChild(loginButton);
    authButtons.appendChild(menuButton);
}

function updateHomeHero(session) {
    const primaryAction = document.getElementById("hero-primary-action");
    const secondaryNote = document.getElementById("hero-secondary-note");
    if (!primaryAction || !secondaryNote) return;

    if (session && session.logged_in) {
        primaryAction.href = "/me.html";
        primaryAction.textContent = "Pokračovat do profilu";
        secondaryNote.textContent = "Jste přihlášeni. Profil, důvěru i další kroky máte připravené hned po ruce.";
        return;
    }

    primaryAction.href = `${API_URL}/auth/login`;
    primaryAction.textContent = "Přihlásit se";
    secondaryNote.textContent = "Přihlášení a správa identity běží bezpečně přes AHOJ420.";
}

async function logoutFlow() {
    const res = await apiPost("/auth/logout");
    if (res && res.idp_logout_url) {
        const alsoAhoj = window.confirm("Odhlásit se i z ahoj420.eu?");
        window.location.href = alsoAhoj ? res.idp_logout_url : "/";
        return;
    }

    window.location.href = "/";
}

async function initIndexPage() {
    const session = await apiGet("/api/session");
    let profile = null;
    if (session && session.logged_in) {
        profile = await apiGet("/api/me");
    }
    setAppIdentity(session, profile);
    renderHeader(session, profile);
    updateHomeHero(session);
}

let activeToastHost = null;

function ensureToastHost() {
    if (activeToastHost?.isConnected) {
        return activeToastHost;
    }

    const host = document.createElement("div");
    host.className = "toast-stack";
    host.setAttribute("aria-live", "polite");
    host.setAttribute("aria-atomic", "true");
    document.body.appendChild(host);
    activeToastHost = host;
    return host;
}

function showToast(text, {
    kind = "info",
    duration = 2200
} = {}) {
    if (!text) {
        return {
            dismiss() {}
        };
    }

    const host = ensureToastHost();
    const toast = document.createElement("div");
    toast.className = "toast";
    if (kind) {
        toast.classList.add(`is-${kind}`);
    }
    toast.textContent = text;
    host.appendChild(toast);

    requestAnimationFrame(() => {
        toast.classList.add("is-visible");
    });

    let removed = false;
    let timeoutID = null;

    const dismiss = () => {
        if (removed) {
            return;
        }
        removed = true;
        if (timeoutID) {
            window.clearTimeout(timeoutID);
        }
        toast.classList.remove("is-visible");
        window.setTimeout(() => {
            toast.remove();
            if (host.childElementCount === 0 && host.parentNode) {
                host.remove();
                if (activeToastHost === host) {
                    activeToastHost = null;
                }
            }
        }, 180);
    };

    if (duration > 0) {
        timeoutID = window.setTimeout(dismiss, duration);
    }

    return {
        dismiss
    };
}

function setStatusMessage(node, text, kind = "") {
    if (!node) return;
    node.textContent = text;
    node.className = "status-message";
    if (kind) {
        node.classList.add(`is-${kind}`);
    }
}

function renderViewedCaptures(captures) {
    const container = document.getElementById("viewed-captures-list");
    if (!container) return;

    const items = Array.isArray(captures) ? captures : [];
    if (!items.length) {
        container.innerHTML = `
            <div class="viewed-capture-empty">
                Zatím jste si za houbičky neodemkli žádné souřadnice.
            </div>
        `;
        return;
    }

    container.innerHTML = items.map((capture) => {
        const badge = buildCaptureAccessBadgeHtml(capture);
        const unlockedAtLabel = hasMeaningfulDateTime(capture.unlocked_at) ? formatDateTime(capture.unlocked_at, "") : "";
        const imageHtml = capture.public_url
            ? buildCaptureImageTag(capture, {
                variant: "thumb",
                alt: "Odemčená fotografie",
                className: "viewed-capture-thumb",
                loading: "lazy",
                sizes: "(max-width: 720px) 100vw, 320px"
            })
            : `<div class="viewed-capture-thumb viewed-capture-thumb-placeholder">Náhled už není veřejný</div>`;

        return `
            <article class="viewed-capture-card">
                <div class="viewed-capture-media">
                    ${imageHtml}
                    ${badge}
                </div>
                <div class="viewed-capture-meta">
                    <div class="viewed-capture-head">
                        <strong>${escapeHtml(capture.author_name || "Neznámý houbař")}</strong>
                        ${unlockedAtLabel ? `<span>${escapeHtml(unlockedAtLabel)}</span>` : ""}
                    </div>
                    <p class="viewed-capture-coordinates">${escapeHtml(formatCaptureCoordinates(capture))}</p>
                    <p class="subtle-note">
                        Odemčeno za 1 houbičku. Fotografie zůstává v soukromém přehledu i po dalším vývoji profilu.
                    </p>
                </div>
            </article>
        `;
    }).join("");
}

async function initMePage() {
    const session = await apiGet("/api/session");
    if (!session || !session.logged_in) {
        window.location.href = "/";
        return;
    }

    const me = await apiGet("/api/me");
    if (!me) return;
    setAppIdentity(session, me);

    renderHeader(session, me);

    const insights = buildProfileInsights(me);
    const bonusRules = [
        "Registrace: +1 houbička",
        me.email_verified ? "E-mail potvrzen: +3 houbičky připsány" : "Potvrzení e-mailu: čeká bonus +3 houbičky",
        me.phone_number_verified ? "Telefon potvrzen: +5 houbiček připsáno" : "Potvrzení telefonu: čeká bonus +5 houbiček"
    ];

    renderProfilePicture("profile-picture", me.picture, "Profilová fotka");
    setText("account-name", me.preferred_username || "Bez uživatelského jména");
    setText("metric-last-visit", getPreviousProfileVisit());
    setText("metric-status", insights.statusLabel);
    setText("metric-bonuses", `${insights.bonusCount} / ${insights.bonusTotal}`);
    setText("metric-notifications", String(insights.alerts.length));
    refreshHoubickaBalanceViews();
    setText("account-email-chip", me.email_verified ? "E-mail · ověřen" : "E-mail · čeká na ověření");
    setText("account-phone-chip", me.phone_number_verified ? "Telefon · ověřen" : "Telefon · čeká na ověření");
    setText("account-sync-chip", "Synchronizováno přes AHOJ420");
    setupNicknameEditor(me);

    const statusPill = document.getElementById("account-status-pill");
    if (statusPill) {
        statusPill.textContent = insights.statusLabel;
        statusPill.className = `status-pill ${insights.tone}`;
    }

    renderSimpleList(
        "alerts-list",
        insights.alerts,
        "Profil je v dobré kondici. Teď už jen udržovat aktivitu."
    );

    renderSimpleList("houbicka-rules", bonusRules, "Bonusová pravidla se načtou později.");

    const publicLink = document.getElementById("profile-public-link");
    if (publicLink) {
        publicLink.href = buildPublicProfileURL(me.id);
    }

    const selfDeleteButton = document.getElementById("self-delete-btn");
    const selfDeleteStatus = document.getElementById("self-delete-status");
    if (selfDeleteButton) {
        if (userCanAdminClient(me) || Number(me.id) === 20) {
            selfDeleteButton.disabled = true;
            setStatusMessage(selfDeleteStatus, "Admin účet nelze mazat přes self-service.", "error");
        } else {
            selfDeleteButton.addEventListener("click", () => handleSelfDeleteAccount(me));
        }
    }

    if (typeof window.initProfileActivityMap === "function") {
        await window.initProfileActivityMap();
    }
}

async function handleSelfDeleteAccount(me) {
    const button = document.getElementById("self-delete-btn");
    const statusNode = document.getElementById("self-delete-status");
    if (!me || !button) {
        return;
    }

    const username = String(me.preferred_username || "").trim();
    const promptLabel = username || `ID ${me.id}`;
    const typedConfirmation = window.prompt(`Smazání je trvalé. Pro potvrzení napište přesně: ${promptLabel}`);
    if (typedConfirmation === null) {
        return;
    }
    if (typedConfirmation.trim() !== promptLabel) {
        setStatusMessage(statusNode, "Potvrzení nesouhlasí. Účet zůstal beze změny.", "error");
        return;
    }
    if (!window.confirm("Opravdu chcete nevratně smazat svůj účet na Houbam Zdar?")) {
        return;
    }

    button.disabled = true;
    setStatusMessage(statusNode, "Mažu váš účet a související data...");

    try {
        const payload = await apiJsonRequest("/api/me", { method: "DELETE" });
        setAppIdentity({ logged_in: false, user: null }, null);
        setStatusMessage(statusNode, "Účet byl smazán. Přesměrovávám...", "success");
        window.setTimeout(() => {
            window.location.href = payload?.redirect_url || "/";
        }, 800);
    } catch (error) {
        console.error("Failed to delete own account", error);
        setStatusMessage(statusNode, error.message || "Účet se nepodařilo smazat.", "error");
        button.disabled = false;
    }
}

function setupAboutEditor(initialValue, onSaved) {
    const saveBtn = document.getElementById("save-about-btn");
    const saveStatus = document.getElementById("save-status");
    const aboutInput = document.getElementById("about-me-input");

    if (!saveBtn || !aboutInput) return;

    aboutInput.value = initialValue || "";

    saveBtn.addEventListener("click", async () => {
        const aboutMeVal = aboutInput.value;
        setStatusMessage(saveStatus, "Ukládám...");

        const res = await apiPost("/api/me/about", { about_me: aboutMeVal });
        if (res && res.ok) {
            setStatusMessage(saveStatus, "Uloženo", "success");
            if (typeof onSaved === "function") {
                onSaved(aboutMeVal);
            }
            window.setTimeout(() => setStatusMessage(saveStatus, ""), 3000);
            return;
        }

        setStatusMessage(saveStatus, "Uložení se nepovedlo", "error");
    });
}

function renderPublicOwnerPanel(visible) {
    const slot = document.getElementById("public-owner-panel-slot");
    if (!slot) {
        return;
    }

    if (!visible) {
        slot.innerHTML = "";
        return;
    }

    slot.innerHTML = `
        <section id="public-owner-panel" class="about-shell card">
            <div class="about-head">
                <div>
                    <p class="section-label">Váš text profilu</p>
                    <h2>Upravte své veřejné představení</h2>
                </div>
                <p class="muted-copy">
                    Napište pár vět o sobě, svých oblíbených lesích nebo plánech na další výpravu.
                </p>
            </div>

            <label class="sr-only" for="about-me-input">Krátké představení</label>
            <textarea id="about-me-input" rows="6" maxlength="2000" placeholder="Napište něco o sobě..."></textarea>

            <div class="action-row">
                <button id="save-about-btn" class="btn btn-primary">Uložit veřejný profil</button>
                <span id="save-status" class="status-message" aria-live="polite"></span>
            </div>
        </section>
    `;
}

function toDatetimeLocalValue(raw) {
    if (!raw) {
        return "";
    }
    const parsed = new Date(raw);
    if (Number.isNaN(parsed.getTime())) {
        return "";
    }
    const local = new Date(parsed.getTime() - (parsed.getTimezoneOffset() * 60_000));
    return local.toISOString().slice(0, 16);
}

function fromDatetimeLocalValue(raw) {
    const value = String(raw || "").trim();
    if (!value) {
        return "";
    }
    const parsed = new Date(value);
    if (Number.isNaN(parsed.getTime())) {
        return "";
    }
    return parsed.toISOString();
}

function formatModerationActionTitle(action) {
    switch (action?.action_kind) {
        case "user_restrictions_updated":
            return "Upravena omezení účtu";
        case "user_roles_updated":
            return "Upraveny role účtu";
        case "capture_ai_review_updated":
            return "Aktualizováno AI rozpoznání fotografie";
        case "capture_taxonomy_updated":
            return "Ručně upraveny taxony fotografie";
        case "capture_geo_updated":
            return "Ručně upravena lokalita fotografie";
        case "capture_visibility_updated":
            return "Upravena viditelnost fotografie";
        case "post_visibility_updated":
            return "Upravena viditelnost publikace";
        case "comment_visibility_updated":
            return "Upravena viditelnost komentáře";
        default:
            return "Moderátorský zásah";
    }
}

function formatModerationActionDetails(action) {
    const bits = [];
    if (action?.reason_code) {
        bits.push(`Důvod: ${action.reason_code}`);
    }
    if (action?.target_capture_id) {
        bits.push("Cíl: fotografie");
    } else if (action?.target_post_id) {
        bits.push("Cíl: publikace");
    } else if (action?.target_comment_id) {
        bits.push("Cíl: komentář");
    } else if (action?.target_user_id) {
        bits.push("Cíl: účet");
    }
    return bits.join(" · ");
}

function renderPublicModeratorPanel(visible) {
    const slot = document.getElementById("public-moderator-panel-slot");
    if (!slot) {
        return;
    }

    if (!visible) {
        slot.innerHTML = "";
        return;
    }

    slot.innerHTML = `
        <section id="public-moderator-panel" class="about-shell card moderator-control-panel">
            <div class="about-head">
                <div>
                    <p class="section-label">Moderace uživatele</p>
                    <h2>Omezení účtu</h2>
                </div>
                <p class="muted-copy">
                    Moderace neodemyká skryté souřadnice. Tyto údaje zůstávají dostupné jen přes běžný houbičkový flow.
                </p>
            </div>

            <div class="form-grid moderator-control-grid">
                <label class="field-block">
                    <span>Ban do</span>
                    <input id="moderation-banned-until" type="datetime-local">
                </label>
                <label class="field-block">
                    <span>Mute komentářů do</span>
                    <input id="moderation-comments-muted-until" type="datetime-local">
                </label>
                <label class="field-block">
                    <span>Stop publikace do</span>
                    <input id="moderation-publishing-suspended-until" type="datetime-local">
                </label>
            </div>

            <label class="field-block">
                <span>Poznámka moderátora</span>
                <textarea id="moderation-note-input" rows="4" maxlength="2000" placeholder="Interní poznámka k zásahu"></textarea>
            </label>

            <div class="action-row">
                <button id="moderation-save-restrictions-btn" type="button" class="btn btn-secondary">Uložit omezení a poznámku</button>
                <span id="moderation-status" class="status-message" aria-live="polite"></span>
            </div>

            <div class="moderation-history-panel">
                <div class="about-head">
                    <div>
                        <p class="section-label">Historie zásahů</p>
                        <h3>Audit log</h3>
                    </div>
                </div>
                <p id="moderation-actions-summary" class="muted-copy">Načítám historii moderace...</p>
                <div id="moderation-actions-list" class="moderation-actions-list">
                    <p class="muted-copy">Načítám historii moderace...</p>
                </div>
                <div class="action-row">
                    <button id="moderation-actions-load-more-btn" type="button" class="btn btn-secondary" style="display: none;">Načíst další zásahy</button>
                </div>
            </div>
        </section>
    `;
}

function syncPublicModeratorPanel() {
    const user = publicProfileState.moderationUser;
    if (!user) {
        return;
    }

    const noteInput = document.getElementById("moderation-note-input");
    const bannedInput = document.getElementById("moderation-banned-until");
    const commentsMutedInput = document.getElementById("moderation-comments-muted-until");
    const publishingSuspendedInput = document.getElementById("moderation-publishing-suspended-until");

    if (noteInput) {
        noteInput.value = user.moderation_note || "";
    }
    if (bannedInput) {
        bannedInput.value = toDatetimeLocalValue(user.banned_until);
    }
    if (commentsMutedInput) {
        commentsMutedInput.value = toDatetimeLocalValue(user.comments_muted_until);
    }
    if (publishingSuspendedInput) {
        publishingSuspendedInput.value = toDatetimeLocalValue(user.publishing_suspended_until);
    }
}

function renderPublicModerationActions() {
    const summaryNode = document.getElementById("moderation-actions-summary");
    const listNode = document.getElementById("moderation-actions-list");
    const loadMoreBtn = document.getElementById("moderation-actions-load-more-btn");
    if (!summaryNode || !listNode) {
        return;
    }

    const actions = publicProfileState.moderationActions || [];
    const total = Number(publicProfileState.moderationActionsTotal || 0);

    summaryNode.textContent = total > 0
        ? `Načteno ${actions.length} z ${total} zásahů.`
        : "Zatím bez zaznamenaných zásahů.";

    if (!actions.length) {
        listNode.innerHTML = '<p class="muted-copy">Zatím bez zaznamenaných zásahů.</p>';
    } else {
        listNode.innerHTML = actions.map((action) => {
            const actorLabel = action.actor_name
                ? `Moderoval ${escapeHtml(action.actor_name)}`
                : `Moderoval uživatel #${escapeHtml(String(action.actor_user_id || ""))}`;
            const details = formatModerationActionDetails(action);
            return `
                <article class="moderation-action-card">
                    <div class="moderation-action-head">
                        <strong>${escapeHtml(formatModerationActionTitle(action))}</strong>
                        <span class="muted-copy">${escapeHtml(formatDateTime(action.created_at))}</span>
                    </div>
                    <p class="muted-copy">${actorLabel}</p>
                    ${details ? `<p class="muted-copy">${escapeHtml(details)}</p>` : ""}
                    ${action.note ? `<p>${escapeHtml(action.note)}</p>` : ""}
                </article>
            `;
        }).join("");
    }

    if (loadMoreBtn) {
        loadMoreBtn.style.display = publicProfileState.moderationActionsHasMore ? "inline-flex" : "none";
    }
}

async function loadPublicModerationActions(append = false) {
    if (!userCanModerateClient(window.appMe) || publicProfileState.isOwner || !publicProfileState.requestedUserID) {
        publicProfileState.moderationActions = [];
        publicProfileState.moderationActionsOffset = 0;
        publicProfileState.moderationActionsTotal = 0;
        publicProfileState.moderationActionsHasMore = false;
        return;
    }

    if (!append) {
        publicProfileState.moderationActions = [];
        publicProfileState.moderationActionsOffset = 0;
        publicProfileState.moderationActionsTotal = 0;
        publicProfileState.moderationActionsHasMore = false;
        renderPublicModerationActions();
    }

    const result = await apiJsonRequest(
        `/api/moderation/users/${encodeURIComponent(publicProfileState.requestedUserID)}/actions?limit=${publicProfileState.moderationActionsLimit}&offset=${publicProfileState.moderationActionsOffset}`
    );
    const actions = Array.isArray(result?.actions) ? result.actions : [];
    publicProfileState.moderationActions = publicProfileState.moderationActions.concat(actions);
    publicProfileState.moderationActionsOffset += actions.length;
    publicProfileState.moderationActionsTotal = Number(result?.total || 0);
    publicProfileState.moderationActionsHasMore = Boolean(result?.has_more);
    renderPublicModerationActions();
}

async function loadPublicModerationUser() {
    if (!userCanModerateClient(window.appMe) || publicProfileState.isOwner || !publicProfileState.requestedUserID) {
        publicProfileState.moderationUser = null;
        publicProfileState.moderationActions = [];
        renderPublicModeratorPanel(false);
        return;
    }

    renderPublicModeratorPanel(true);

    try {
        const result = await apiJsonRequest(`/api/moderation/users/${encodeURIComponent(publicProfileState.requestedUserID)}`);
        if (!result || !result.ok || !result.user) {
            throw new Error("Nepodařilo se načíst stav moderace uživatele.");
        }
        publicProfileState.moderationUser = result.user;
        syncPublicModeratorPanel();
        attachPublicModeratorPanelHandlers();
        await loadPublicModerationActions(false);
    } catch (error) {
        console.error("Failed to load moderation user", error);
        const status = document.getElementById("moderation-status");
        setStatusMessage(status, error.message || "Nepodařilo se načíst stav moderace.", "error");
    }
}

function attachPublicModeratorPanelHandlers() {
    const restrictionsBtn = document.getElementById("moderation-save-restrictions-btn");
    const actionsLoadMoreBtn = document.getElementById("moderation-actions-load-more-btn");
    const status = document.getElementById("moderation-status");

    if (restrictionsBtn && !restrictionsBtn.dataset.bound) {
        restrictionsBtn.dataset.bound = "1";
        restrictionsBtn.addEventListener("click", async () => {
            try {
                restrictionsBtn.disabled = true;
                setStatusMessage(status, "Ukládám omezení a poznámku...");

                const payload = await apiJsonRequest(
                    `/api/moderation/users/${encodeURIComponent(publicProfileState.requestedUserID)}/restrictions`,
                    {
                        method: "POST",
                        body: {
                            banned_until: fromDatetimeLocalValue(document.getElementById("moderation-banned-until")?.value),
                            comments_muted_until: fromDatetimeLocalValue(document.getElementById("moderation-comments-muted-until")?.value),
                            publishing_suspended_until: fromDatetimeLocalValue(document.getElementById("moderation-publishing-suspended-until")?.value),
                            note: document.getElementById("moderation-note-input")?.value || "",
                            reason_code: "manual_moderation"
                        }
                    }
                );
                publicProfileState.moderationUser = payload.user || null;
                syncPublicModeratorPanel();
                await loadPublicModerationActions(false);
                setStatusMessage(status, "Omezení a poznámka uloženy.", "success");
            } catch (error) {
                console.error("Failed to save user restrictions", error);
                setStatusMessage(status, error.message || "Omezení a poznámku se nepodařilo uložit.", "error");
            } finally {
                restrictionsBtn.disabled = false;
            }
        });
    }

    if (actionsLoadMoreBtn && !actionsLoadMoreBtn.dataset.bound) {
        actionsLoadMoreBtn.dataset.bound = "1";
        actionsLoadMoreBtn.addEventListener("click", async () => {
            try {
                actionsLoadMoreBtn.disabled = true;
                await loadPublicModerationActions(true);
            } catch (error) {
                console.error("Failed to load more moderation actions", error);
                setStatusMessage(status, error.message || "Nepodařilo se načíst další zásahy.", "error");
            } finally {
                actionsLoadMoreBtn.disabled = false;
            }
        });
    }
}

const publicProfileState = {
    requestedUserID: 0,
    isOwner: false,
    user: null,
    moderationUser: null,
    moderationActions: [],
    moderationActionsLimit: 8,
    moderationActionsOffset: 0,
    moderationActionsTotal: 0,
    moderationActionsHasMore: false,
    posts: [],
    captures: [],
    galleryCaptures: [],
    postsLimit: 6,
    postsOffset: 0,
    postsHasMore: false,
    galleryPage: 1,
    galleryPageSize: 24,
    galleryTotal: 0,
    galleryTotalPages: 0,
    postsLoaded: false,
    postsLoading: false,
    capturesLoaded: false,
    capturesLoading: false,
    galleryLoaded: false,
    galleryLoading: false,
    map: null,
    markerLayer: null
};

function publicProfileSectionNodes() {
    return {
        mapButton: document.getElementById("public-profile-open-map-btn"),
        postsButton: document.getElementById("public-profile-open-posts-btn"),
        galleryButton: document.getElementById("public-profile-open-gallery-btn"),
        mapSection: document.getElementById("public-profile-map-section"),
        postsSection: document.getElementById("public-profile-posts-section"),
        gallerySection: document.getElementById("public-profile-gallery-section")
    };
}

function syncPublicProfileSectionButtons() {
    const nodes = publicProfileSectionNodes();
    nodes.mapButton?.classList.toggle("is-active", Boolean(nodes.mapSection && !nodes.mapSection.hidden));
    nodes.postsButton?.classList.toggle("is-active", Boolean(nodes.postsSection && !nodes.postsSection.hidden));
    nodes.galleryButton?.classList.toggle("is-active", Boolean(nodes.gallerySection && !nodes.gallerySection.hidden));
}

function setPublicProfileSectionVisibility(section, visible) {
    const nodes = publicProfileSectionNodes();
    const sectionNode = {
        map: nodes.mapSection,
        posts: nodes.postsSection,
        gallery: nodes.gallerySection
    }[section] || null;
    if (!sectionNode) {
        return;
    }
    sectionNode.hidden = !visible;
}

async function openPublicProfileSection(section) {
    const nodes = publicProfileSectionNodes();
    const sectionNode = {
        map: nodes.mapSection,
        posts: nodes.postsSection,
        gallery: nodes.gallerySection
    }[section] || null;
    if (!sectionNode) {
        return;
    }

    setPublicProfileSectionVisibility("map", section === "map");
    setPublicProfileSectionVisibility("posts", section === "posts");
    setPublicProfileSectionVisibility("gallery", section === "gallery");
    syncPublicProfileSectionButtons();

    if (section === "map") {
        if (!publicProfileState.capturesLoaded && !publicProfileState.capturesLoading) {
            await loadPublicProfileCaptures();
        } else {
            renderPublicProfileMap();
            window.setTimeout(() => publicProfileState.map?.invalidateSize(), 0);
        }
    } else if (section === "posts") {
        if (!publicProfileState.postsLoaded && !publicProfileState.postsLoading) {
            await loadPublicProfilePosts(false);
        }
    } else if (!publicProfileState.galleryLoaded && !publicProfileState.galleryLoading) {
        await loadPublicProfileGallery(1);
    } else {
        renderPublicProfileGallery();
    }

    sectionNode.scrollIntoView({ behavior: "smooth", block: "start" });
}

function buildPublicProfileGalleryPaginationItems(currentPage, totalPages) {
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

function parsePublicProfileGalleryPage(rawValue, fallback = 1) {
    const value = Number.parseInt(String(rawValue || ""), 10);
    if (!Number.isFinite(value) || value <= 0) {
        return fallback;
    }
    return value;
}

function formatPublicProfileGalleryRegionLabel(region) {
    const safeRegion = escapeHtml(region || "");
    return safeRegion.replace(/\s+([^\s]+)$/u, "<br>$1");
}

function buildPublicProfileGallerySpeciesButton(capture) {
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

function closePublicProfileGallerySpeciesModal() {
    const modal = document.getElementById("public-profile-gallery-species-modal");
    if (!modal) {
        return;
    }
    modal.hidden = true;
    modal.setAttribute("aria-hidden", "true");
}

function openPublicProfileGallerySpeciesModal(captureID) {
    const modal = document.getElementById("public-profile-gallery-species-modal");
    const body = document.getElementById("public-profile-gallery-species-body");
    const meta = document.getElementById("public-profile-gallery-species-meta");
    if (!modal || !body || !meta) {
        return;
    }

    const capture = publicProfileState.galleryCaptures.find((item) => item && item.id === captureID) || null;
    const entries = buildCaptureSpeciesEntries(capture);
    if (!capture || entries.length === 0) {
        return;
    }

    const authorName = String(capture.author_name || publicProfileState.user?.preferred_username || "Neznámý houbař").trim();
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

function renderPublicProfileGalleryPagination() {
    const pagination = document.getElementById("public-gallery-pagination");
    if (!pagination) {
        return;
    }

    if (publicProfileState.galleryTotalPages <= 1) {
        pagination.hidden = true;
        pagination.innerHTML = "";
        return;
    }

    const items = buildPublicProfileGalleryPaginationItems(publicProfileState.galleryPage, publicProfileState.galleryTotalPages);
    pagination.hidden = false;
    pagination.innerHTML = [
        `<button type="button" class="btn btn-secondary" data-page="${publicProfileState.galleryPage - 1}" ${publicProfileState.galleryPage <= 1 ? "disabled" : ""}>Předchozí</button>`,
        ...items.map((item) => {
            if (item === "gap") {
                return '<span class="gallery-pagination-gap" aria-hidden="true">…</span>';
            }
            const active = item === publicProfileState.galleryPage;
            return `
                <button
                    type="button"
                    class="btn ${active ? "btn-primary" : "btn-secondary"}"
                    data-page="${item}"
                    ${active ? "aria-current=\"page\" disabled" : ""}
                >${item}</button>
            `;
        }),
        `<button type="button" class="btn btn-secondary" data-page="${publicProfileState.galleryPage + 1}" ${publicProfileState.galleryPage >= publicProfileState.galleryTotalPages ? "disabled" : ""}>Další</button>`
    ].join("");

    pagination.querySelectorAll("[data-page]").forEach((button) => {
        button.addEventListener("click", async () => {
            const nextPage = parsePublicProfileGalleryPage(button.dataset.page, publicProfileState.galleryPage);
            if (
                nextPage === publicProfileState.galleryPage
                || nextPage < 1
                || nextPage > publicProfileState.galleryTotalPages
                || publicProfileState.galleryLoading
            ) {
                return;
            }
            await loadPublicProfileGallery(nextPage);
            document.getElementById("public-gallery-container")?.scrollIntoView({ behavior: "smooth", block: "start" });
        });
    });
}

function renderPublicProfileGallery() {
    const container = document.getElementById("public-gallery-container");
    const summary = document.getElementById("public-gallery-summary");
    if (!container || !summary) {
        return;
    }

    const captures = Array.isArray(publicProfileState.galleryCaptures) ? publicProfileState.galleryCaptures : [];
    const authorName = String(publicProfileState.user?.preferred_username || "uživatele").trim();

    if (publicProfileState.galleryLoading && captures.length === 0) {
        summary.textContent = "Načítám fotografie...";
        container.innerHTML = '<p class="muted-copy gallery-grid-status">Načítám fotografie...</p>';
        renderPublicProfileGalleryPagination();
        return;
    }

    if (!captures.length) {
        summary.textContent = `Uživatel ${authorName} zatím nemá žádné zveřejněné fotografie.`;
        container.innerHTML = '<p class="muted-copy gallery-grid-status">Zatím tu nejsou žádné zveřejněné fotografie.</p>';
        renderPublicProfileGalleryPagination();
        return;
    }

    if (publicProfileState.galleryTotalPages > 1) {
        summary.textContent = `Nalezeno ${publicProfileState.galleryTotal} veřejných fotografií od ${authorName}. Zobrazuji stranu ${publicProfileState.galleryPage} z ${publicProfileState.galleryTotalPages}.`;
    } else {
        summary.textContent = `Nalezeno ${publicProfileState.galleryTotal} veřejných fotografií od ${authorName}.`;
    }

    container.innerHTML = captures.map((capture, idx) => {
        const avatarUrl = capture.author_avatar || DEFAULT_AVATAR_URL;
        const captureAuthorName = capture.author_name || publicProfileState.user?.preferred_username || "Neznámý houbař";
        const accessBadge = buildCaptureAccessBadgeHtml(capture);
        const authorURL = buildPublicProfileURL(capture.author_user_id);
        const region = buildCaptureKrajLabel(capture);
        const imageHtml = buildCaptureImageTag(capture, {
            variant: "thumb",
            alt: "Houbařský úlovek",
            loading: "lazy",
            sizes: "(max-width: 720px) 50vw, (max-width: 1200px) 33vw, 384px"
        });
        const speciesButton = buildPublicProfileGallerySpeciesButton(capture);

        return `
            <div class="gallery-item" data-index="${idx}" tabindex="0" role="button" aria-label="Zobrazit detail fotky">
                <div class="gallery-item-header">
                    <a href="${escapeHtml(authorURL)}" class="author-link">
                        <img src="${escapeHtml(avatarUrl)}" class="gallery-item-avatar" alt="Avatar">
                        <span class="gallery-item-author">${escapeHtml(captureAuthorName)}</span>
                    </a>
                </div>
                <div class="gallery-item-image">
                    ${imageHtml}
                    ${accessBadge}
                </div>
                <div class="gallery-item-copy">
                    ${region ? `
                        <div class="gallery-item-meta-row">
                            <p class="gallery-item-region">${formatPublicProfileGalleryRegionLabel(region)}</p>
                        </div>
                    ` : ""}
                </div>
                ${speciesButton}
            </div>
        `;
    }).join("");

    container.querySelectorAll(".gallery-item").forEach((item) => {
        const openItemLightbox = () => {
            if (!window.HZDLightbox) {
                return;
            }
            window.HZDLightbox.openCollection(publicProfileState.galleryCaptures, Number(item.dataset.index || 0));
        };
        item.addEventListener("click", openItemLightbox);
        item.addEventListener("keydown", (event) => {
            if (event.key === "Enter" || event.key === " ") {
                event.preventDefault();
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
            openPublicProfileGallerySpeciesModal(button.dataset.captureId);
        });
    });

    renderPublicProfileGalleryPagination();
}

async function loadPublicProfileGallery(page = publicProfileState.galleryPage) {
    const container = document.getElementById("public-gallery-container");
    if (!container || !publicProfileState.requestedUserID || publicProfileState.galleryLoading) {
        return;
    }

    let loadSucceeded = false;
    publicProfileState.galleryLoading = true;
    publicProfileState.galleryPage = Number.isFinite(Number(page)) && Number(page) > 0 ? Number(page) : 1;
    renderPublicProfileGallery();

    try {
        const limit = publicProfileState.galleryPageSize;
        const offset = (publicProfileState.galleryPage - 1) * limit;
        const result = await apiGet(`/api/public/users/${encodeURIComponent(publicProfileState.requestedUserID)}/captures?limit=${limit}&offset=${offset}`);
        if (!result || !result.ok) {
            throw new Error("Nepodařilo se načíst galerii uživatele.");
        }

        publicProfileState.galleryCaptures = Array.isArray(result.captures) ? result.captures : [];
        publicProfileState.galleryTotal = Number.isFinite(Number(result.total)) ? Number(result.total) : 0;
        publicProfileState.galleryTotalPages = publicProfileState.galleryTotal > 0
            ? Math.max(1, Math.ceil(publicProfileState.galleryTotal / publicProfileState.galleryPageSize))
            : 0;

        if (publicProfileState.galleryTotalPages > 0 && publicProfileState.galleryPage > publicProfileState.galleryTotalPages) {
            publicProfileState.galleryLoading = false;
            await loadPublicProfileGallery(publicProfileState.galleryTotalPages);
            return;
        }

        publicProfileState.galleryLoaded = true;
        loadSucceeded = true;
    } catch (error) {
        console.error("Failed to load public profile gallery", error);
        publicProfileState.galleryCaptures = [];
        publicProfileState.galleryTotal = 0;
        publicProfileState.galleryTotalPages = 0;
        const summary = document.getElementById("public-gallery-summary");
        if (summary) {
            summary.textContent = error.message || "Galerii se nepodařilo načíst.";
        }
        container.innerHTML = `<p class="muted-copy gallery-grid-status">${escapeHtml(error.message || "Galerii se nepodařilo načíst.")}</p>`;
        renderPublicProfileGalleryPagination();
    } finally {
        publicProfileState.galleryLoading = false;
        if (loadSucceeded) {
            renderPublicProfileGallery();
        }
    }
}

function resolveRequestedPublicProfileUserID(params, me) {
    const requestedParam = params.get("user");
    if (requestedParam !== null) {
        const parsed = Number.parseInt(requestedParam, 10);
        if (!Number.isSafeInteger(parsed) || parsed <= 0) {
            return 0;
        }
        return parsed;
    }

    const ownID = Number(me && me.id);
    if (!Number.isSafeInteger(ownID) || ownID <= 0) {
        return 0;
    }

    return ownID;
}

function buildPublicTrustProfile(profile) {
    const score =
        (profile.preferred_username ? 28 : 0) +
        (profile.picture ? 22 : 0) +
        (profile.about_me ? 18 : 0) +
        (profile.email_verified ? 16 : 0) +
        (profile.phone_verified ? 16 : 0);

    let trustLabel = "Rozpracovaný profil";
    if (score >= 80) {
        trustLabel = "Silný veřejný profil";
    } else if (score >= 55) {
        trustLabel = "Důvěryhodný profil";
    }

    return {
        score: Math.min(score, 100),
        trustLabel
    };
}

function buildCapturePopupPreviewHtml(capture, altText) {
    const imageHtml = capture?.public_url
        ? buildCaptureImageTag(capture, {
            variant: "popup",
            alt: altText,
            loading: "lazy"
        })
        : "";
    if (!imageHtml) {
        return '<div class="map-popup-placeholder">Bez veřejného náhledu</div>';
    }

    return imageHtml;
}

function buildCaptureMapMarkerLabel(capture, fallback = "Otevřít fotografii") {
    if (!capture) {
        return fallback;
    }

    const species = buildCaptureSpeciesLabel(capture);
    const authorName = String(capture.author_name || capture.post_author_name || "").trim();
    return species || authorName || fallback;
}

function buildCaptureMapMarkerTooltipHtml({ title = "", metaLines = [] } = {}) {
    const safeTitle = escapeHtml(title || "Fotografie");
    const details = (Array.isArray(metaLines) ? metaLines : [])
        .map((line) => String(line || "").trim())
        .filter(Boolean)
        .join(" • ");

    return `
        <div class="hzd-map-marker-tooltip">
            <strong>${safeTitle}</strong>
            ${details ? `<span>${escapeHtml(details)}</span>` : ""}
        </div>
    `;
}

function createCaptureMapMarker(capture, {
    title = "",
    tooltipHtml = "",
    onActivate = null
} = {}) {
    const lat = Number(capture?.latitude);
    const lon = Number(capture?.longitude);
    const label = title || buildCaptureMapMarkerLabel(capture);
    const markerImageHtml = buildCaptureImageTag(capture, {
        variant: "mapMarker",
        alt: "",
        loading: "lazy",
        sizes: "56px",
        extraAttrs: {
            "aria-hidden": "true"
        }
    }) || '<span class="hzd-map-thumb-marker__placeholder" aria-hidden="true">?</span>';

    const marker = L.marker([lat, lon], {
        icon: L.divIcon({
            className: "hzd-map-thumb-icon",
            html: `
                <div class="hzd-map-thumb-marker" role="button" aria-label="${escapeHtml(label)}">
                    <span class="hzd-map-thumb-marker__frame">
                        ${markerImageHtml}
                    </span>
                    <span class="hzd-map-thumb-marker__pin" aria-hidden="true"></span>
                </div>
            `,
            iconSize: [58, 74],
            iconAnchor: [29, 72],
            tooltipAnchor: [0, -42],
            popupAnchor: [0, -54]
        }),
        title: label,
        alt: label,
        riseOnHover: true
    });

    if (tooltipHtml) {
        marker.bindTooltip(tooltipHtml, {
            direction: "top",
            offset: [0, -42],
            opacity: 1,
            className: "hzd-map-marker-tooltip-shell"
        });
    }

    if (typeof onActivate === "function") {
        marker.on("click", () => {
            onActivate(capture);
        });
    }

    return marker;
}

function buildSharedMapPopupHtml({
    authorName = "Neznámý houbař",
    authorUrl = "",
    previewUrl = "",
    previewHtml = "",
    altText = "",
    dateValue = "",
    metaLines = [],
    actionHtml = ""
    } = {}) {
    const safeAuthor = escapeHtml(authorName || "Neznámý houbař");
    const resolvedPreviewHtml = previewHtml
        || (previewUrl
            ? `<img src="${escapeHtml(previewUrl)}" alt="${escapeHtml(altText || authorName || "Fotografie")}" loading="lazy" decoding="async">`
            : "")
        || '<div class="map-popup-placeholder">Bez veřejného náhledu</div>';
    const titleHtml = authorUrl
        ? `<h4><a href="${escapeHtml(authorUrl)}">${safeAuthor}</a></h4>`
        : `<h4>${safeAuthor}</h4>`;
    const detailsHtml = (Array.isArray(metaLines) ? metaLines : [])
        .filter(Boolean)
        .map((line) => `<p>${escapeHtml(line)}</p>`)
        .join("");

    return `
        <div class="map-popup-content">
            ${resolvedPreviewHtml}
            ${titleHtml}
            ${dateValue ? `<p>${escapeHtml(formatDateTime(dateValue))}</p>` : ""}
            ${detailsHtml}
            ${actionHtml || ""}
        </div>
    `;
}

function bindMapPopupAction(marker, selector, handler) {
    if (!marker || typeof marker.on !== "function" || !selector || typeof handler !== "function") {
        return;
    }

    marker.on("popupopen", () => {
        const popupNode = marker.getPopup()?.getElement();
        const button = popupNode?.querySelector(selector);
        if (!button) {
            return;
        }
        button.onclick = (event) => {
            event.preventDefault();
            event.stopPropagation();
            handler(event, button);
        };
    });
}

window.HZDMapUI = {
    buildPopupHtml: buildSharedMapPopupHtml,
    bindPopupAction: bindMapPopupAction,
    buildMarkerTooltipHtml: buildCaptureMapMarkerTooltipHtml,
    createCaptureMarker: createCaptureMapMarker
};

function buildPublicProfileMapPopupHtml(capture) {
    const authorName = capture.author_name || publicProfileState.user?.preferred_username || "Neznámý houbař";
    const canOpenLightbox = Boolean(capture.public_url || publicProfileState.isOwner);
    return buildSharedMapPopupHtml({
        authorName,
        previewUrl: !capture.public_url && publicProfileState.isOwner ? buildCaptureImageURL(capture, "popup") : "",
        previewHtml: capture.public_url ? buildCapturePopupPreviewHtml(capture, authorName) : "",
        altText: authorName,
        dateValue: capture.captured_at,
        actionHtml: canOpenLightbox
            ? `<button type="button" class="btn btn-secondary map-popup-action public-profile-map-open-btn" data-capture-id="${escapeHtml(capture.id)}">Otevřít ve fotkách</button>`
            : ""
    });
}

function ensurePublicProfileMap() {
    const mapNode = document.getElementById("public-map");
    if (!mapNode || typeof L === "undefined") {
        return null;
    }

    if (!publicProfileState.map) {
        publicProfileState.map = L.map("public-map").setView([49.8, 15.5], 7);
        L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
            attribution: "&copy; OpenStreetMap"
        }).addTo(publicProfileState.map);
        publicProfileState.markerLayer = L.layerGroup().addTo(publicProfileState.map);
    }

    return publicProfileState.map;
}

function openPublicProfileMapLightbox(captureID) {
    const capturesToOpen = publicProfileState.captures.filter((capture) => capture && (capture.public_url || publicProfileState.isOwner));
    const startIndex = capturesToOpen.findIndex((capture) => capture.id === captureID);
    if (startIndex === -1 || !window.HZDLightbox) {
        return;
    }

    window.HZDLightbox.openCollection(capturesToOpen, startIndex);
}

function renderPublicProfileMap() {
    const map = ensurePublicProfileMap();
    const emptyNode = document.getElementById("public-map-empty");
    const summaryNode = document.getElementById("public-map-summary");
    const captures = publicProfileState.captures.filter((capture) => captureHasCoordinates(capture));

    if (summaryNode) {
        summaryNode.textContent = `${captures.length} z ${publicProfileState.captures.length} načtených fotografií má souřadnice.`;
    }

    if (!map || !emptyNode) {
        return;
    }

    publicProfileState.markerLayer?.clearLayers();

    if (!captures.length) {
        emptyNode.hidden = false;
        emptyNode.textContent = "Zatím tu nejsou žádné veřejné fotografie s polohou.";
        return;
    }

    emptyNode.hidden = true;
    const markers = captures.map((capture) => {
        const markerTitle = buildCaptureSpeciesLabel(capture) || capture.author_name || "Otevřít fotografii";
        const markerTooltip = buildCaptureMapMarkerTooltipHtml({
            title: markerTitle,
            metaLines: [capture.author_name || "", formatDateTime(capture.captured_at)]
        });

        if (window.HZDMapUI?.createCaptureMarker) {
            return window.HZDMapUI.createCaptureMarker(capture, {
                title: markerTitle,
                tooltipHtml: markerTooltip,
                onActivate: () => {
                    openPublicProfileMapLightbox(capture.id);
                }
            });
        }

        const marker = L.marker([Number(capture.latitude), Number(capture.longitude)]);
        marker.bindPopup(buildPublicProfileMapPopupHtml(capture));
        if (window.HZDMapUI) {
            window.HZDMapUI.bindPopupAction(marker, ".public-profile-map-open-btn", () => {
                openPublicProfileMapLightbox(capture.id);
            });
        }
        return marker;
    });

    if (window.HZDMapClusters) {
        publicProfileState.markerLayer = window.HZDMapClusters.replaceLayer(
            map,
            publicProfileState.markerLayer,
            markers,
            {
                clusterOptions: {
                    maxClusterRadius: 56,
                    spiderfyDistanceMultiplier: 1.24
                }
            }
        );
        window.HZDMapClusters.fitLayer(map, publicProfileState.markerLayer, { padding: [30, 30], maxZoom: 15 });
        return;
    }

    publicProfileState.markerLayer = L.featureGroup(markers).addTo(map);
    if (publicProfileState.markerLayer.getBounds().isValid()) {
        map.fitBounds(publicProfileState.markerLayer.getBounds(), { padding: [30, 30], maxZoom: 15 });
    }
}

async function loadPublicProfilePosts(append = false) {
    const container = document.getElementById("public-posts-container");
    const loadMoreButton = document.getElementById("public-posts-load-more-btn");
    const summaryNode = document.getElementById("public-posts-summary");
    if (!container || !publicProfileState.requestedUserID || publicProfileState.postsLoading) {
        return;
    }

    publicProfileState.postsLoading = true;

    if (!append) {
        publicProfileState.posts = [];
        publicProfileState.postsOffset = 0;
        container.innerHTML = '<p class="muted-copy">Načítám publikace...</p>';
    }

    try {
        const result = await apiGet(`/api/public/users/${encodeURIComponent(publicProfileState.requestedUserID)}/posts?limit=${publicProfileState.postsLimit}&offset=${publicProfileState.postsOffset}`);
        if (!result || !result.ok) {
            container.innerHTML = '<p class="muted-copy">Nepodařilo se načíst publikace.</p>';
            if (loadMoreButton) loadMoreButton.style.display = "none";
            return;
        }

        const posts = result.posts || [];
        publicProfileState.posts = publicProfileState.posts.concat(posts);
        publicProfileState.postsOffset += posts.length;
        publicProfileState.postsHasMore = Boolean(result.has_more);
        publicProfileState.postsLoaded = true;

        container.innerHTML = "";
        if (!publicProfileState.posts.length) {
            container.innerHTML = '<p class="muted-copy">Zatím žádné publikace.</p>';
        } else if (window.hzdFeedUI && typeof window.hzdFeedUI.renderPosts === "function") {
            window.hzdFeedUI.renderPosts(publicProfileState.posts, container, {
                postsStore: publicProfileState.posts,
                allowPostManagement: publicProfileState.isOwner,
                onPostDeleted: (_, nextPosts) => {
                    if (summaryNode) {
                        summaryNode.textContent = `Načteno ${nextPosts.length} publikací.`;
                    }
                    if (!nextPosts.length) {
                        container.innerHTML = '<p class="muted-copy">Zatím žádné publikace.</p>';
                    }
                }
            });
        }

        if (summaryNode) {
            summaryNode.textContent = `Načteno ${publicProfileState.posts.length} z ${result.total || publicProfileState.posts.length} publikací.`;
        }
        if (loadMoreButton) {
            loadMoreButton.style.display = publicProfileState.postsHasMore ? "inline-flex" : "none";
        }
    } finally {
        publicProfileState.postsLoading = false;
    }
}

async function loadPublicProfileCaptures() {
    if (!publicProfileState.requestedUserID || publicProfileState.capturesLoading) {
        return;
    }

    publicProfileState.capturesLoading = true;
    publicProfileState.captures = [];

    try {
        const result = await apiGet(`/api/public/users/${encodeURIComponent(publicProfileState.requestedUserID)}/map-captures`);
        if (!result || !result.ok) {
            const emptyNode = document.getElementById("public-map-empty");
            if (emptyNode) {
                emptyNode.hidden = false;
                emptyNode.textContent = "Nepodařilo se načíst veřejnou mapu.";
            }
            return;
        }

        publicProfileState.captures = Array.isArray(result.captures) ? result.captures : [];
        publicProfileState.capturesLoaded = true;
        renderPublicProfileMap();
    } finally {
        publicProfileState.capturesLoading = false;
    }
}

async function initPublicProfilePage() {
    const session = await apiGet("/api/session");
    let me = null;
    if (session && session.logged_in) {
        me = await apiGet("/api/me");
    }
    setAppIdentity(session, me);
    renderHeader(session, me);

    publicProfileState.user = null;
    publicProfileState.isOwner = false;
    publicProfileState.moderationUser = null;
    publicProfileState.moderationActions = [];
    publicProfileState.moderationActionsOffset = 0;
    publicProfileState.moderationActionsTotal = 0;
    publicProfileState.moderationActionsHasMore = false;
    publicProfileState.posts = [];
    publicProfileState.captures = [];
    publicProfileState.galleryCaptures = [];
    publicProfileState.postsOffset = 0;
    publicProfileState.postsHasMore = false;
    publicProfileState.galleryPage = 1;
    publicProfileState.galleryTotal = 0;
    publicProfileState.galleryTotalPages = 0;
    publicProfileState.postsLoaded = false;
    publicProfileState.postsLoading = false;
    publicProfileState.capturesLoaded = false;
    publicProfileState.capturesLoading = false;
    publicProfileState.galleryLoaded = false;
    publicProfileState.galleryLoading = false;
    renderPublicModeratorPanel(false);
    renderPublicOwnerPanel(false);
    setPublicProfileSectionVisibility("map", false);
    setPublicProfileSectionVisibility("posts", false);
    setPublicProfileSectionVisibility("gallery", false);
    syncPublicProfileSectionButtons();

    const params = new URLSearchParams(window.location.search);
    const requestedUserID = resolveRequestedPublicProfileUserID(params, me);
    if (!requestedUserID) {
        setText("public-profile-name", "Profil nenalezen");
        setText("public-profile-trust", "Odkaz na veřejný profil je neplatný nebo chybí identita uživatele.");
        return;
    }

    publicProfileState.requestedUserID = requestedUserID;

    const profileRes = await apiGet(`/api/public/users/${encodeURIComponent(requestedUserID)}`);
    if (!profileRes || !profileRes.ok || !profileRes.user) {
        setText("public-profile-name", "Profil nenalezen");
        setText("public-profile-trust", "Tento veřejný profil se nepodařilo načíst.");
        return;
    }

    const profile = profileRes.user;
    publicProfileState.user = profile;
    publicProfileState.isOwner = Boolean(
        me &&
        Number.isSafeInteger(Number(me.id)) &&
        Number(me.id) === Number(profile.id) &&
        Number(profile.id) === requestedUserID
    );

    const trust = buildPublicTrustProfile(profile);
    renderProfilePicture("public-profile-picture", profile.picture, "Veřejná profilová fotka");
    setText("public-profile-name", profile.preferred_username || "Bez veřejného jména");
    setText("public-profile-trust", `${trust.trustLabel}. Mapu, publikace a galerii otevřete tlačítky nahoře.`);
    setText("trust-score", `${trust.score} %`);
    setText("trust-label", trust.trustLabel);
    setText("public-about-preview", profile.about_me || "Zatím bez veřejného představení.");
    setText("public-profile-stats", `${profile.public_posts_count || 0} publikací · ${profile.public_captures_count || 0} veřejných fotografií`);

    const trustFill = document.getElementById("trust-bar-fill");
    if (trustFill) {
        trustFill.style.width = `${trust.score}%`;
    }

    if (publicProfileState.isOwner) {
        renderPublicOwnerPanel(true);
        setupAboutEditor(profile.about_me || "", (value) => {
            setText("public-about-preview", value || "Zatím bez veřejného představení.");
        });
    }

    const postsLoadMore = document.getElementById("public-posts-load-more-btn");
    if (postsLoadMore) {
        postsLoadMore.addEventListener("click", () => loadPublicProfilePosts(true));
    }
    const speciesModal = document.getElementById("public-profile-gallery-species-modal");
    const speciesModalClose = document.getElementById("public-profile-gallery-species-close");
    if (speciesModal && !speciesModal.dataset.bound) {
        speciesModal.dataset.bound = "1";
        speciesModal.addEventListener("click", (event) => {
            if (event.target instanceof HTMLElement && event.target.hasAttribute("data-close-public-gallery-species-modal")) {
                closePublicProfileGallerySpeciesModal();
            }
        });
    }
    if (speciesModalClose && !speciesModalClose.dataset.bound) {
        speciesModalClose.dataset.bound = "1";
        speciesModalClose.addEventListener("click", closePublicProfileGallerySpeciesModal);
    }
    if (!window.__hzdPublicProfileGallerySpeciesEscBound) {
        window.__hzdPublicProfileGallerySpeciesEscBound = true;
        window.addEventListener("keydown", (event) => {
            if (event.key === "Escape") {
                closePublicProfileGallerySpeciesModal();
            }
        });
    }
    const sectionNodes = publicProfileSectionNodes();
    if (sectionNodes.mapButton && !sectionNodes.mapButton.dataset.bound) {
        sectionNodes.mapButton.dataset.bound = "1";
        sectionNodes.mapButton.addEventListener("click", () => {
            openPublicProfileSection("map");
        });
    }
    if (sectionNodes.postsButton && !sectionNodes.postsButton.dataset.bound) {
        sectionNodes.postsButton.dataset.bound = "1";
        sectionNodes.postsButton.addEventListener("click", () => {
            openPublicProfileSection("posts");
        });
    }
    if (sectionNodes.galleryButton && !sectionNodes.galleryButton.dataset.bound) {
        sectionNodes.galleryButton.dataset.bound = "1";
        sectionNodes.galleryButton.addEventListener("click", () => {
            openPublicProfileSection("gallery");
        });
    }
    syncPublicProfileSectionButtons();

    await loadPublicModerationUser();
}

// Společná Lightbox logika
window.lightboxImages = [];
window.lightboxMapData = []; // [{lat: 12.3, lon: 45.6}, null, ...]
window.lightboxCaptureData = [];
window.currentLightboxIndex = 0;
let captureMapViewerInstance = null;
let captureMapViewerMarkerLayer = null;

function openSharedLightboxCollection(captures, startIndex = 0, options = {}) {
    const list = Array.isArray(captures) ? captures.filter(Boolean) : [];
    const index = Number(startIndex);
    const imageBuilder = typeof options.imageBuilder === "function"
        ? options.imageBuilder
        : (capture) => buildCaptureImageURL(capture, "lightbox");
    const mapBuilder = typeof options.mapBuilder === "function"
        ? options.mapBuilder
        : (capture) => buildCaptureMapData(capture);

    if (!list.length || !Number.isInteger(index) || index < 0 || index >= list.length) {
        return false;
    }

    window.lightboxImages = list.map((capture) => imageBuilder(capture));
    window.lightboxCaptureData = list.slice();
    window.lightboxMapData = list.map((capture) => mapBuilder(capture));
    window.currentLightboxIndex = index;

    if (typeof openLightbox === "function") {
        openLightbox();
        return true;
    }

    return false;
}

window.HZDLightbox = {
    openCollection: openSharedLightboxCollection
};

function currentLightboxCapture() {
    return window.lightboxCaptureData[window.currentLightboxIndex] || null;
}

function setLightboxMessage(text, kind = "") {
    const node = document.getElementById("lightbox-note");
    if (!node) return;

    node.textContent = text || "";
    node.className = "lightbox-note";
    if (kind) {
        node.classList.add(`is-${kind}`);
    }
}

function syncLightboxImage() {
    const img = document.getElementById("lightbox-img");
    if (!img || window.lightboxImages.length === 0) return;
    const capture = currentLightboxCapture();
    if (capture?.public_url) {
        const altText = buildCaptureSpeciesLabel(capture) || capture.author_name || "Detail fotky";
        setCaptureImageElement(img, capture, {
            variant: "lightbox",
            alt: altText,
            fetchPriority: "high"
        });
        window.lightboxImages[window.currentLightboxIndex] = buildCaptureImageURL(capture, "lightbox");
        return;
    }

    img.src = window.lightboxImages[window.currentLightboxIndex];
    img.alt = capture?.author_name || "Detail fotky";
    img.decoding = "async";
    img.setAttribute("fetchpriority", "high");
    img.removeAttribute("srcset");
    img.removeAttribute("sizes");
}

function ensureCaptureMapViewer() {
    const viewer = document.getElementById("capture-map-viewer");
    const mapNode = document.getElementById("capture-map-viewer-map");
    if (!viewer || !mapNode || typeof L === "undefined") {
        return null;
    }

    if (!captureMapViewerInstance) {
        captureMapViewerInstance = L.map("capture-map-viewer-map").setView([49.8, 15.5], 7);
        L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
            attribution: "&copy; OpenStreetMap"
        }).addTo(captureMapViewerInstance);
        captureMapViewerMarkerLayer = L.layerGroup().addTo(captureMapViewerInstance);
    }

    return {
        viewer,
        map: captureMapViewerInstance,
        markerLayer: captureMapViewerMarkerLayer
    };
}

function closeCaptureMapViewer() {
    const viewer = document.getElementById("capture-map-viewer");
    if (!viewer) {
        return;
    }

    viewer.classList.remove("active");
    viewer.hidden = true;
}

function openCaptureMapViewer(data, capture = null, options = {}) {
    if (!data || Number.isNaN(data.lat) || Number.isNaN(data.lon)) {
        return false;
    }

    const viewerState = ensureCaptureMapViewer();
    const noteNode = document.getElementById("capture-map-viewer-note");
    if (!viewerState) {
        return false;
    }

    const { viewer, map, markerLayer } = viewerState;
    const locationLabel = buildCaptureRegionLabel(capture);
    const customNote = String(options.note || "").trim();

    if (noteNode) {
        if (customNote) {
            noteNode.textContent = customNote;
        } else if (capture?.coordinates_free) {
            noteNode.textContent = locationLabel
                ? `Veřejně sdílené souřadnice. ${locationLabel}.`
                : "Veřejně sdílené souřadnice této fotografie.";
        } else if (hasMeaningfulDateTime(capture?.unlocked_at)) {
            noteNode.textContent = locationLabel
                ? `Odemčené souřadnice. ${locationLabel}.`
                : `Souřadnice odemčeny ${formatDateTime(capture.unlocked_at)}.`;
        } else {
            noteNode.textContent = locationLabel || "Přesná poloha fotografie.";
        }
    }

    if (markerLayer && typeof markerLayer.clearLayers === "function") {
        markerLayer.clearLayers();
        L.marker([data.lat, data.lon]).addTo(markerLayer);
    }

    viewer.hidden = false;
    viewer.classList.add("active");
    map.setView([data.lat, data.lon], 13);
    window.setTimeout(() => map.invalidateSize(), 0);
    return true;
}

async function unlockCurrentLightboxCapture() {
    const capture = currentLightboxCapture();
    const mapBtn = document.getElementById("lightbox-map-btn");

    if (!capture || !capture.id || !capture.coordinates_locked || !mapBtn) {
        return;
    }

    if (!window.appSession || !window.appSession.logged_in) {
        window.location.href = `${API_URL}/auth/login`;
        return;
    }

    mapBtn.disabled = true;
    setLightboxMessage("Odemykám souřadnice za 1 houbičku...");

    try {
        const res = await fetch(`${API_URL}/api/captures/${encodeURIComponent(capture.id)}/unlock-coordinates`, {
            method: "POST",
            credentials: "include"
        });

        if (res.status === 402) {
            setLightboxMessage("Na odemčení nemáte dost houbiček.", "error");
            return;
        }
        if (res.status === 404) {
            setLightboxMessage("Fotografie už není pro odemčení dostupná.", "error");
            return;
        }
        if (res.status === 400) {
            setLightboxMessage("Tato fotografie nemá GPS souřadnice.", "error");
            return;
        }
        if (!res.ok) {
            throw new Error(`HTTP error ${res.status}`);
        }

        const payload = await res.json();
        if (!payload || !payload.ok || !payload.capture) {
            throw new Error("Invalid unlock response");
        }

        const updatedCapture = payload.capture;
        window.lightboxCaptureData[window.currentLightboxIndex] = updatedCapture;
        window.lightboxImages[window.currentLightboxIndex] = buildCaptureImageURL(updatedCapture, "lightbox");
        window.lightboxMapData[window.currentLightboxIndex] = buildCaptureMapData(updatedCapture);

        if (window.appMe && typeof payload.balance === "number") {
            window.appMe.houbicka_balance = payload.balance;
            refreshHoubickaBalanceViews();
        }

        if (typeof window.onLightboxCaptureUpdated === "function") {
            window.onLightboxCaptureUpdated(updatedCapture, window.currentLightboxIndex);
        }

        syncLightboxImage();
        const successMessage = (
            payload.already_unlocked
                ? "Souřadnice už jste měli k dispozici."
                : `Souřadnice odemčeny. Zůstatek: ${formatHoubickaCount(payload.balance)}.`
        );
        const updatedMapData = buildCaptureMapData(updatedCapture);
        const openedMapViewer = openCaptureMapViewer(updatedMapData, updatedCapture, {
            note: successMessage
        });
        if (openedMapViewer) {
            closeLightbox();
            return;
        }
        setLightboxMessage(successMessage, "success");
        updateLightboxMap();
    } catch (error) {
        console.error("Failed to unlock coordinates", error);
        setLightboxMessage("Souřadnice se nepodařilo odemknout.", "error");
    } finally {
        mapBtn.disabled = false;
    }
}

function updateLightboxMap() {
    const mapBtn = document.getElementById("lightbox-map-btn");
    if (!mapBtn) return;

    const capture = currentLightboxCapture();
    const data = capture ? buildCaptureMapData(capture) : window.lightboxMapData[window.currentLightboxIndex];
    const locationLabel = buildCaptureRegionLabel(capture);

    mapBtn.style.display = "none";
    mapBtn.disabled = false;
    mapBtn.onclick = null;
    setLightboxMessage("");

    if (!capture) {
        return;
    }

    if (capture.coordinates_locked) {
        mapBtn.style.display = "block";
        if (window.appSession && window.appSession.logged_in) {
            mapBtn.textContent = "Otevřít souřadnice za 1 houbičku";
            mapBtn.onclick = (event) => {
                event.stopPropagation();
                unlockCurrentLightboxCapture();
            };
            setLightboxMessage(
                locationLabel
                    ? `Kraj: ${locationLabel}. Po odemčení se fotografie uloží do přehledu „Prohlédnuté za houbičky“.`
                    : "Po odemčení se fotografie uloží do přehledu „Prohlédnuté za houbičky“."
            );
        } else {
            mapBtn.textContent = "Přihlásit se pro souřadnice";
            mapBtn.onclick = (event) => {
                event.stopPropagation();
                window.location.href = `${API_URL}/auth/login`;
            };
            setLightboxMessage(
                locationLabel
                    ? `Kraj: ${locationLabel}. Souřadnice si mohou odemykat jen přihlášení uživatelé.`
                    : "Souřadnice si mohou odemykat jen přihlášení uživatelé."
            );
        }
        return;
    }

    if (!data || Number.isNaN(data.lat) || Number.isNaN(data.lon)) {
        if (locationLabel) {
            setLightboxMessage(`Lokalita: ${locationLabel}.`, "success");
        }
        return;
    }

    mapBtn.style.display = "block";
    mapBtn.textContent = "Zobrazit na mapě";

    if (capture.coordinates_free) {
        setLightboxMessage(
            locationLabel
                ? `Lokalita: ${locationLabel}. Souřadnice této fotografie jsou zdarma.`
                : "Souřadnice této fotografie jsou zdarma.",
            "success"
        );
    } else if (hasMeaningfulDateTime(capture.unlocked_at)) {
        setLightboxMessage(
            locationLabel
                ? `Lokalita: ${locationLabel}. Souřadnice byly odemčeny ${formatDateTime(capture.unlocked_at)}.`
                : `Souřadnice byly odemčeny ${formatDateTime(capture.unlocked_at)}.`,
            "success"
        );
    } else if (locationLabel) {
        setLightboxMessage(`Lokalita: ${locationLabel}.`, "success");
    }

    mapBtn.onclick = (event) => {
        event.stopPropagation();
        closeLightbox();
        openCaptureMapViewer(data, capture);
    };
}

function openLightbox() {
    const lb = document.getElementById("lightbox");
    if (!lb || window.lightboxImages.length === 0) return;
    closeCaptureMapViewer();
    syncLightboxImage();
    updateLightboxMap();
    lb.classList.add("active");
}

function closeLightbox() {
    const lb = document.getElementById("lightbox");
    if (lb) lb.classList.remove("active");
    setLightboxMessage("");
}

function lightboxNext() {
    if (window.lightboxImages.length === 0) return;
    window.currentLightboxIndex = (window.currentLightboxIndex + 1) % window.lightboxImages.length;
    syncLightboxImage();
    updateLightboxMap();
}

function lightboxPrev() {
    if (window.lightboxImages.length === 0) return;
    window.currentLightboxIndex = (window.currentLightboxIndex - 1 + window.lightboxImages.length) % window.lightboxImages.length;
    syncLightboxImage();
    updateLightboxMap();
}

document.addEventListener("DOMContentLoaded", () => {
    if (!document.getElementById("lightbox")) {
        const lightboxHTML = `
    <div id="lightbox" class="lightbox">
        <button type="button" id="lightbox-close" class="lightbox-close" aria-label="Zavřít galerii">&times;</button>
        <button type="button" id="lightbox-prev" class="lightbox-nav lightbox-prev" aria-label="Předchozí fotka">&#10094;</button>
        <img id="lightbox-img" class="lightbox-content" src="" alt="Detail fotky">
        <button type="button" id="lightbox-next" class="lightbox-nav lightbox-next" aria-label="Další fotka">&#10095;</button>
        <button type="button" id="lightbox-map-btn" class="btn btn-primary lightbox-map-btn" style="display: none;">Zobrazit na mapě</button>
        <p id="lightbox-note" class="lightbox-note"></p>
    </div>
        `;
        document.body.insertAdjacentHTML('beforeend', lightboxHTML);
    }

    if (!document.getElementById("capture-map-viewer")) {
        const mapViewerHTML = `
    <div id="capture-map-viewer" class="capture-map-viewer" hidden>
        <div class="capture-map-viewer-backdrop" data-close-capture-map></div>
        <div class="capture-map-viewer-dialog" role="dialog" aria-modal="true" aria-label="Mapa lokality">
            <button type="button" id="capture-map-viewer-close" class="capture-map-viewer-close" aria-label="Zavřít mapu">&times;</button>
            <div class="capture-map-viewer-head">
                <p class="section-label">Mapa lokality</p>
            </div>
            <p id="capture-map-viewer-note" class="capture-map-viewer-note muted-copy"></p>
            <div id="capture-map-viewer-map" class="capture-map-viewer-map"></div>
        </div>
    </div>
        `;
        document.body.insertAdjacentHTML("beforeend", mapViewerHTML);
    }

    const lbClose = document.getElementById("lightbox-close");
    const lbNext = document.getElementById("lightbox-next");
    const lbPrev = document.getElementById("lightbox-prev");
    const lb = document.getElementById("lightbox");
    const mapViewer = document.getElementById("capture-map-viewer");
    const mapViewerClose = document.getElementById("capture-map-viewer-close");
    const mapViewerBackdrop = document.querySelector("[data-close-capture-map]");

    if (lbClose) lbClose.addEventListener("click", closeLightbox);
    if (lbNext) lbNext.addEventListener("click", (e) => { e.stopPropagation(); lightboxNext(); });
    if (lbPrev) lbPrev.addEventListener("click", (e) => { e.stopPropagation(); lightboxPrev(); });
    if (lb) lb.addEventListener("click", (e) => {
        if (e.target === lb) closeLightbox();
    });
    if (mapViewerClose) mapViewerClose.addEventListener("click", closeCaptureMapViewer);
    if (mapViewerBackdrop) mapViewerBackdrop.addEventListener("click", closeCaptureMapViewer);
    if (mapViewer) mapViewer.addEventListener("click", (e) => {
        if (e.target === mapViewer) closeCaptureMapViewer();
    });

    document.addEventListener("keydown", (e) => {
        const lightboxActive = Boolean(lb && lb.classList.contains("active"));
        const mapViewerActive = Boolean(mapViewer && !mapViewer.hidden);
        if (!lightboxActive && !mapViewerActive) return;
        if (e.key === "Escape") {
            if (mapViewerActive) {
                closeCaptureMapViewer();
                return;
            }
            closeLightbox();
            return;
        }
        if (!lightboxActive) return;
        if (e.key === "ArrowRight") lightboxNext();
        if (e.key === "ArrowLeft") lightboxPrev();
    });
});

async function initCreatePostPage() {
    const session = await apiGet("/api/session");
    if (!session || !session.logged_in) {
        window.location.href = "/";
        return;
    }

    const me = await apiGet("/api/me");
    if (!me) return;
    setAppIdentity(session, me);

    renderHeader(session, me);
    setText(
        "create-post-description",
        `${me.preferred_username || "Váš účet"} má už připravené místo pro budoucí publikace. Tady přidáme editor pro příspěvky, houbařské nálezy a krátké novinky.`
    );
}

async function performReauth() {
    const status = document.getElementById("reauth-status");
    setStatusMessage(status, "Odhlašuji lokální relaci...");

    try {
        await fetch(`${API_URL}/auth/logout`, {
            method: "POST",
            credentials: "include"
        });

        setStatusMessage(status, "Přesměrovávám na nové přihlášení...");
        window.location.href = `${API_URL}/auth/login?next=me`;
    } catch (e) {
        console.error("Reauth failed", e);
        setStatusMessage(status, "Zkuste to prosím znovu.", "error");
    }
}

function initReauthPage() {
    const button = document.getElementById("reauth-btn");
    if (!button) return;
    button.addEventListener("click", performReauth);
}

document.addEventListener("DOMContentLoaded", () => {
    const page = document.body.dataset.page;
    if (page === "home") {
        initIndexPage();
        return;
    }

    if (page === "me") {
        initMePage();
        return;
    }

    if (page === "reauth") {
        initReauthPage();
        return;
    }

    if (page === "public-profile") {
        initPublicProfilePage();
        return;
    }

    if (page === "map") {
        initSharedHeaderOnly();
        return;
    }

    if (page === "create-post") {
        initCreatePostPage();
    }
});
