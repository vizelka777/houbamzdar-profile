const API_URL = "https://api.houbamzdar.cz";
const DEFAULT_AVATAR_URL = "/default-avatar.png";
const PROFILE_LAST_VISIT_KEY = "hzd_last_profile_visit_at";

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

function createLinkButton(label, href, className) {
    const link = document.createElement("a");
    link.className = `btn ${className}`;
    link.href = href;
    link.textContent = label;
    return link;
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

        authButtons.appendChild(greeting);
        authButtons.appendChild(createLinkButton("Vytvořit publikaci", "/create-post.html", "btn-secondary"));
        authButtons.appendChild(createLinkButton("Vyfotit nález", "/capture.html", "btn-secondary"));

        authButtons.appendChild(createLinkButton("Můj profil", "/me.html", "btn-primary"));
        authButtons.appendChild(createActionButton("Odhlásit", "btn-secondary", logoutFlow));
        return;
    }

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

async function initMePage() {
    const session = await apiGet("/api/session");
    if (!session || !session.logged_in) {
        window.location.href = "/";
        return;
    }

    const me = await apiGet("/api/me");
    if (!me) return;

    renderHeader(session, me);

    const insights = buildProfileInsights(me);
    renderProfilePicture("profile-picture", me.picture, "Profilová fotka");
    setText("account-name", me.preferred_username || "Bez uživatelského jména");
    setText("metric-last-visit", getPreviousProfileVisit());
    setText("metric-status", insights.statusLabel);
    setText("metric-bonuses", `${insights.bonusCount} / ${insights.bonusTotal}`);
    setText("metric-notifications", String(insights.alerts.length));
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

    renderSimpleList(
        "views-list",
        [],
        "Zatím bez návštěv veřejného profilu. Jakmile se objeví, uvidíte je tady."
    );
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

async function initPublicProfilePage() {
    const session = await apiGet("/api/session");
    if (!session || !session.logged_in) {
        window.location.href = "/";
        return;
    }

    const me = await apiGet("/api/me");
    if (!me) return;

    renderHeader(session, me);
    renderProfilePicture("public-profile-picture", me.picture, "Veřejná profilová fotka");
    setText("public-profile-name", me.preferred_username || "Bez veřejného jména");

    const insights = buildProfileInsights(me);
    setText(
        "public-profile-trust",
        `${insights.trustLabel}. Veřejné představení pomáhá ostatním poznat, kdo jste.`
    );
    setText("trust-score", `${insights.score} %`);
    setText("trust-label", insights.trustLabel);
    const trustFill = document.getElementById("trust-bar-fill");
    if (trustFill) {
        trustFill.style.width = `${insights.score}%`;
    }
    setText(
        "public-about-preview",
        me.about_me || "Zatím bez veřejného představení. Doplňte pár vět, ať profil působí živěji."
    );

    setupAboutEditor(me.about_me || "", (value) => {
        setText(
            "public-about-preview",
            value || "Zatím bez veřejného představení. Doplňte pár vět, ať profil působí živěji."
        );
    });
}

async function initCreatePostPage() {
    const session = await apiGet("/api/session");
    if (!session || !session.logged_in) {
        window.location.href = "/";
        return;
    }

    const me = await apiGet("/api/me");
    if (!me) return;

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
