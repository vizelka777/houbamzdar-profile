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

function createHeaderMenuButton(label, iconSVG, className, items) {
    const details = document.createElement("details");
    details.className = "header-menu";

    const summary = document.createElement("summary");
    summary.className = `btn ${className} btn-icon btn-icon-labeled`;
    summary.setAttribute("aria-label", label);
    summary.innerHTML = `
        <span class="btn-icon-glyph" aria-hidden="true">${iconSVG}</span>
        <span class="btn-icon-label">${escapeHtml(label)}</span>
    `;
    details.appendChild(summary);

    const panel = document.createElement("div");
    panel.className = "header-menu-panel";

    items.forEach((item) => {
        const link = document.createElement("a");
        link.className = "header-menu-item";
        link.href = item.href;
        link.innerHTML = `
            <span>${escapeHtml(item.label)}</span>
            ${item.note ? `<small class="header-menu-note">${escapeHtml(item.note)}</small>` : ""}
        `;
        panel.appendChild(link);
    });

    details.appendChild(panel);
    document.addEventListener("click", (event) => {
        if (!details.contains(event.target)) {
            details.removeAttribute("open");
        }
    });

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

function formatDateTime(dateString) {
    if (!dateString) return "Právě teď";
    const date = new Date(dateString);
    if (Number.isNaN(date.getTime())) return "Právě teď";
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

function buildCaptureImageURL(capture) {
    if (!capture) return "";
    if (capture.public_url) return capture.public_url;
    return `${API_URL}/api/captures/${encodeURIComponent(capture.id)}/preview`;
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

    if (!user.email_verified) {
        alerts.push("Ověřte e-mail, ať účet působí důvěryhodněji.");
    }

    if (!user.phone_number) {
        alerts.push("Doplňte telefon, ať vás ostatní snadno poznají.");
    } else if (!user.phone_number_verified) {
        alerts.push("Ověřte telefon, ať se profil posune výš.");
    }

    if (!user.picture) {
        alerts.push("Přidejte profilovou fotku pro silnější důvěru.");
    }

    if (!user.about_me) {
        alerts.push("Doplňte krátké veřejné představení.");
    }

    const bonuses = [
        Boolean(user.picture),
        Boolean(user.about_me),
        Boolean(user.email_verified),
        Boolean(user.phone_number && user.phone_number_verified)
    ];

    const score =
        (user.preferred_username ? 15 : 0) +
        (user.email ? 10 : 0) +
        (user.email_verified ? 20 : 0) +
        (user.phone_number ? 10 : 0) +
        (user.phone_number_verified ? 20 : 0) +
        (user.picture ? 15 : 0) +
        (user.about_me ? 10 : 0);

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

function renderHeader(session, profile = null) {
    const authButtons = document.getElementById("auth-buttons");
    if (!authButtons) return;

    authButtons.innerHTML = "";

    if (session && session.logged_in) {
        const greeting = document.createElement("span");
        greeting.className = "user-greeting";
        greeting.textContent = `Ahoj, ${session.user?.preferred_username || "hoste"}`;

        const composeIcon = `
            <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M6 3h8l4 4v12a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2zm7 1.5V8h3.5"></path>
                <path d="M9 14.5 15.2 8.3a1.4 1.4 0 0 1 2 2L11 16.5l-2.7.7z"></path>
            </svg>
        `;
        const cameraIcon = `
            <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M4 7h3l1.4-2h7.2L17 7h3a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V9a2 2 0 0 1 2-2zm8 2.5A4.5 4.5 0 1 0 12 18a4.5 4.5 0 0 0 0-9zm0 2A2.5 2.5 0 1 1 12 16a2.5 2.5 0 0 1 0-5z"></path>
            </svg>
        `;
        const photoToolsIcon = `
            <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M4 7.5h8m4 0h4M9 7.5a2 2 0 1 1-4 0 2 2 0 0 1 4 0Zm10 0a2 2 0 1 1-4 0 2 2 0 0 1 4 0ZM4 16.5h4m4 0h8M13 16.5a2 2 0 1 1-4 0 2 2 0 0 1 4 0Zm10 0a2 2 0 1 1-4 0 2 2 0 0 1 4 0Z"></path>
            </svg>
        `;

        authButtons.appendChild(greeting);
        authButtons.appendChild(createLabeledIconLinkButton("/create-post.html", "Vytvořit publikaci", composeIcon, "btn-primary"));
        authButtons.appendChild(createDirectCameraButton("Vyfotit nový nález", cameraIcon, "btn-secondary"));
        authButtons.appendChild(createHeaderMenuButton("Foto", photoToolsIcon, "btn-secondary", [
            {
                href: "/capture.html",
                label: "Zpracování fotek",
                note: "lokální snímky, výběr a nahrání na server"
            },
            {
                href: "/server-storage.html",
                label: "Serverový archiv",
                note: "to, co už je uložené v Bunny"
            }
        ]));
        authButtons.appendChild(createLinkButton("Zeď úlovků", "/feed.html", "btn-secondary"));
        authButtons.appendChild(createLinkButton("Galerie", "/gallery.html", "btn-secondary"));
        authButtons.appendChild(createLinkButton("Mapa", "/map.html", "btn-secondary"));

        authButtons.appendChild(createLinkButton("Můj profil", "/me.html", "btn-primary"));
        authButtons.appendChild(createActionButton("Odhlásit", "btn-secondary", logoutFlow));
        return;
    }

    authButtons.appendChild(createLinkButton("Zeď úlovků", "/feed.html", "btn-secondary"));
    authButtons.appendChild(createLinkButton("Galerie", "/gallery.html", "btn-secondary"));
    authButtons.appendChild(createLinkButton("Mapa", "/map.html", "btn-secondary"));
    authButtons.appendChild(
        createLinkButton("Přihlášení / registrace", `${API_URL}/auth/login`, "btn-primary")
    );
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
        const imageUrl = buildCaptureImageURL(capture);
        const badge = buildCaptureAccessBadgeHtml(capture);
        const imageHtml = capture.public_url
            ? `<img src="${escapeHtml(imageUrl)}" alt="Odemčená fotografie" class="viewed-capture-thumb" loading="lazy">`
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
                        <span>${escapeHtml(formatDateTime(capture.unlocked_at))}</span>
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
    setText("account-email-chip", me.email ? `E-mail · ${me.email_verified ? "ověřen" : "čeká na ověření"}` : "E-mail chybí");
    setText("account-phone-chip", me.phone_number ? `Telefon · ${me.phone_number_verified ? "ověřen" : "čeká na ověření"}` : "Telefon chybí");
    setText("account-sync-chip", "Synchronizováno přes AHOJ420");

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

    if (typeof window.initProfileActivityMap === "function") {
        await window.initProfileActivityMap();
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

const publicProfileState = {
    requestedUserID: 0,
    isOwner: false,
    user: null,
    posts: [],
    captures: [],
    postsLimit: 6,
    capturesLimit: 60,
    postsOffset: 0,
    capturesOffset: 0,
    postsHasMore: false,
    capturesHasMore: false,
    map: null,
    markerLayer: null
};

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
    const imageUrl = capture?.public_url ? buildCaptureImageURL(capture) : "";
    if (!imageUrl) {
        return '<div class="map-popup-placeholder">Bez veřejného náhledu</div>';
    }

    return `<img src="${escapeHtml(imageUrl)}" alt="${escapeHtml(altText)}" loading="lazy">`;
}

function buildPublicProfileMapPopupHtml(capture) {
    const authorName = capture.author_name || publicProfileState.user?.preferred_username || "Neznámý houbař";
    const imageUrl = capture.public_url ? buildCaptureImageURL(capture) : "";
    const previewHtml = imageUrl
        ? `<a href="${escapeHtml(imageUrl)}" target="_blank" rel="noreferrer">${buildCapturePopupPreviewHtml(capture, authorName)}</a>`
        : buildCapturePopupPreviewHtml(capture, authorName);
    return `
        <div class="map-popup-content">
            ${previewHtml}
            <h4>${escapeHtml(authorName)}</h4>
            <p>${escapeHtml(formatDateTime(capture.captured_at))}</p>
        </div>
    `;
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

function renderPublicProfileMap() {
    const map = ensurePublicProfileMap();
    const emptyNode = document.getElementById("public-map-empty");
    const summaryNode = document.getElementById("public-map-summary");
    const loadMoreButton = document.getElementById("public-map-load-more-btn");
    const captures = publicProfileState.captures.filter((capture) => captureHasCoordinates(capture));

    if (summaryNode) {
        summaryNode.textContent = `${captures.length} z ${publicProfileState.captures.length} načtených fotografií má souřadnice.`;
    }
    if (loadMoreButton) {
        loadMoreButton.style.display = publicProfileState.capturesHasMore ? "inline-flex" : "none";
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
        const marker = L.marker([Number(capture.latitude), Number(capture.longitude)]);
        marker.bindPopup(buildPublicProfileMapPopupHtml(capture));
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
    if (!container || !publicProfileState.requestedUserID) {
        return;
    }

    if (!append) {
        publicProfileState.posts = [];
        publicProfileState.postsOffset = 0;
        container.innerHTML = '<p class="muted-copy">Načítám publikace...</p>';
    }

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
}

async function loadPublicProfileCaptures(append = false) {
    if (!publicProfileState.requestedUserID) {
        return;
    }

    if (!append) {
        publicProfileState.captures = [];
        publicProfileState.capturesOffset = 0;
    }

    const result = await apiGet(`/api/public/users/${encodeURIComponent(publicProfileState.requestedUserID)}/captures?limit=${publicProfileState.capturesLimit}&offset=${publicProfileState.capturesOffset}`);
    if (!result || !result.ok) {
        const emptyNode = document.getElementById("public-map-empty");
        if (emptyNode) {
            emptyNode.hidden = false;
            emptyNode.textContent = "Nepodařilo se načíst veřejnou mapu.";
        }
        return;
    }

    const captures = result.captures || [];
    publicProfileState.captures = publicProfileState.captures.concat(captures);
    publicProfileState.capturesOffset += captures.length;
    publicProfileState.capturesHasMore = Boolean(result.has_more);
    renderPublicProfileMap();
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
    const ownerPanel = document.getElementById("public-owner-panel");
    if (ownerPanel) {
        ownerPanel.hidden = true;
    }

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
    setText("public-profile-trust", `${trust.trustLabel}. Veřejné publikace a mapa se načítají níže.`);
    setText("trust-score", `${trust.score} %`);
    setText("trust-label", trust.trustLabel);
    setText("public-about-preview", profile.about_me || "Zatím bez veřejného představení.");
    setText("public-profile-stats", `${profile.public_posts_count || 0} publikací · ${profile.public_captures_count || 0} veřejných fotografií`);

    const trustFill = document.getElementById("trust-bar-fill");
    if (trustFill) {
        trustFill.style.width = `${trust.score}%`;
    }

    if (ownerPanel) {
        ownerPanel.hidden = !publicProfileState.isOwner;
    }

    if (publicProfileState.isOwner) {
        setupAboutEditor(profile.about_me || "", (value) => {
            setText("public-about-preview", value || "Zatím bez veřejného představení.");
        });
    }

    const postsLoadMore = document.getElementById("public-posts-load-more-btn");
    if (postsLoadMore) {
        postsLoadMore.addEventListener("click", () => loadPublicProfilePosts(true));
    }

    const mapLoadMore = document.getElementById("public-map-load-more-btn");
    if (mapLoadMore) {
        mapLoadMore.addEventListener("click", () => loadPublicProfileCaptures(true));
    }

    await Promise.all([
        loadPublicProfilePosts(false),
        loadPublicProfileCaptures(false)
    ]);
}

// Společná Lightbox logika
window.lightboxImages = [];
window.lightboxMapData = []; // [{lat: 12.3, lon: 45.6}, null, ...]
window.lightboxCaptureData = [];
window.currentLightboxIndex = 0;
let lightboxMapInstance = null;

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
    img.src = window.lightboxImages[window.currentLightboxIndex];
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
        window.lightboxImages[window.currentLightboxIndex] = buildCaptureImageURL(updatedCapture);
        window.lightboxMapData[window.currentLightboxIndex] = buildCaptureMapData(updatedCapture);

        if (window.appMe && typeof payload.balance === "number") {
            window.appMe.houbicka_balance = payload.balance;
            refreshHoubickaBalanceViews();
        }

        if (typeof window.onLightboxCaptureUpdated === "function") {
            window.onLightboxCaptureUpdated(updatedCapture, window.currentLightboxIndex);
        }

        syncLightboxImage();
        setLightboxMessage(
            payload.already_unlocked
                ? "Souřadnice už jste měli k dispozici."
                : `Souřadnice odemčeny. Zůstatek: ${formatHoubickaCount(payload.balance)}.`,
            "success"
        );
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
    const mapDiv = document.getElementById("lightbox-map");
    if (!mapBtn || !mapDiv) return;

    const capture = currentLightboxCapture();
    const data = capture ? buildCaptureMapData(capture) : window.lightboxMapData[window.currentLightboxIndex];

    mapBtn.style.display = "none";
    mapBtn.disabled = false;
    mapDiv.style.display = "none";
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
            setLightboxMessage("Po odemčení se fotografie uloží do přehledu „Prohlédnuté za houbičky“.");
        } else {
            mapBtn.textContent = "Přihlásit se pro souřadnice";
            mapBtn.onclick = (event) => {
                event.stopPropagation();
                window.location.href = `${API_URL}/auth/login`;
            };
            setLightboxMessage("Souřadnice si mohou odemykat jen přihlášení uživatelé.");
        }
        return;
    }

    if (!data || Number.isNaN(data.lat) || Number.isNaN(data.lon)) {
        return;
    }

    mapBtn.style.display = "block";
    mapBtn.textContent = "Zobrazit na mapě";

    if (capture.coordinates_free) {
        setLightboxMessage("Souřadnice této fotografie jsou zdarma.", "success");
    } else if (capture.unlocked_at) {
        setLightboxMessage(`Souřadnice byly odemčeny ${formatDateTime(capture.unlocked_at)}.`, "success");
    }

    mapBtn.onclick = (event) => {
        event.stopPropagation();

        if (mapDiv.style.display === "none") {
            mapDiv.style.display = "block";
            mapBtn.textContent = "Skrýt mapu";

            if (!lightboxMapInstance && typeof L !== "undefined") {
                lightboxMapInstance = L.map("lightbox-map").setView([data.lat, data.lon], 13);
                L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
                    attribution: "&copy; OpenStreetMap"
                }).addTo(lightboxMapInstance);
                L.marker([data.lat, data.lon]).addTo(lightboxMapInstance);
            } else if (lightboxMapInstance) {
                lightboxMapInstance.setView([data.lat, data.lon], 13);
                lightboxMapInstance.eachLayer((layer) => {
                    if (layer instanceof L.Marker) {
                        lightboxMapInstance.removeLayer(layer);
                    }
                });
                L.marker([data.lat, data.lon]).addTo(lightboxMapInstance);
                lightboxMapInstance.invalidateSize();
            }
            return;
        }

        mapDiv.style.display = "none";
        mapBtn.textContent = "Zobrazit na mapě";
    };
}

function openLightbox() {
    const lb = document.getElementById("lightbox");
    if (!lb || window.lightboxImages.length === 0) return;
    syncLightboxImage();
    updateLightboxMap();
    lb.classList.add("active");
}

function closeLightbox() {
    const lb = document.getElementById("lightbox");
    if (lb) lb.classList.remove("active");
    const mapDiv = document.getElementById("lightbox-map");
    if (mapDiv) mapDiv.style.display = "none";
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
    const lbClose = document.getElementById("lightbox-close");
    const lbNext = document.getElementById("lightbox-next");
    const lbPrev = document.getElementById("lightbox-prev");
    const lb = document.getElementById("lightbox");

    if (lbClose) lbClose.addEventListener("click", closeLightbox);
    if (lbNext) lbNext.addEventListener("click", (e) => { e.stopPropagation(); lightboxNext(); });
    if (lbPrev) lbPrev.addEventListener("click", (e) => { e.stopPropagation(); lightboxPrev(); });
    if (lb) lb.addEventListener("click", (e) => {
        if (e.target === lb) closeLightbox();
    });

    document.addEventListener("keydown", (e) => {
        if (!lb || !lb.classList.contains("active")) return;
        if (e.key === "Escape") closeLightbox();
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

    if (page === "create-post") {
        initCreatePostPage();
    }
});
