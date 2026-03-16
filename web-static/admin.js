const adminPageState = {
    session: null,
    me: null,
    overview: null,
    users: [],
    total: 0,
    page: 1,
    pageSize: 12,
    loadingUsers: false,
    backups: {
        items: [],
        total: 0,
        limit: 8,
        offset: 0,
        hasMore: false,
        loading: false,
        running: false
    },
    filters: {
        query: "",
        role: "",
        status: ""
    }
};

function formatOptionalDateTime(value) {
    if (!value) {
        return "";
    }
    return formatDateTime(value);
}

function adminRestrictionBadges(user) {
    const badges = [];
    if (user?.is_banned) {
        badges.push(`<span class="capture-access-badge capture-access-badge-paid">Ban do ${escapeHtml(formatOptionalDateTime(user.banned_until))}</span>`);
    }
    if (user?.comments_muted) {
        badges.push(`<span class="capture-access-badge capture-access-badge-map">Komentáře do ${escapeHtml(formatOptionalDateTime(user.comments_muted_until))}</span>`);
    }
    if (user?.publishing_suspended) {
        badges.push(`<span class="capture-access-badge capture-access-badge-paid">Publikace do ${escapeHtml(formatOptionalDateTime(user.publishing_suspended_until))}</span>`);
    }
    return badges.join("");
}

function adminRoleBadges(user) {
    const badges = [];
    if (user?.is_admin) {
        badges.push('<span class="capture-access-badge capture-access-badge-free">Admin</span>');
    }
    if (user?.is_moderator) {
        badges.push('<span class="capture-access-badge capture-access-badge-map">Moderátor</span>');
    }
    if (!badges.length) {
        badges.push('<span class="capture-access-badge">Uživatel</span>');
    }
    return badges.join("");
}

function renderAdminOverview() {
    const overview = adminPageState.overview || {};
    setText("admin-stat-total-users", String(overview.total_users || 0));
    setText("admin-stat-staff-users", String(overview.staff_users || 0));
    setText("admin-stat-restricted-users", String(overview.restricted_users || 0));
    setText("admin-stat-banned-users", String(overview.banned_users || 0));
}

function renderAdminUsersSummary() {
    const summary = document.getElementById("admin-users-summary");
    const pageLabel = document.getElementById("admin-users-page-label");
    const prevButton = document.getElementById("admin-users-prev");
    const nextButton = document.getElementById("admin-users-next");
    if (!summary || !pageLabel || !prevButton || !nextButton) {
        return;
    }

    const totalPages = Math.max(1, Math.ceil(adminPageState.total / adminPageState.pageSize));
    if (adminPageState.loadingUsers && adminPageState.users.length === 0) {
        summary.textContent = "Načítám uživatele...";
    } else if (adminPageState.total === 0) {
        summary.textContent = "Filtrům neodpovídá žádný účet.";
    } else {
        summary.textContent = `Načteno ${adminPageState.users.length} z ${adminPageState.total} účtů.`;
    }

    pageLabel.textContent = `Strana ${adminPageState.page} z ${totalPages}`;
    prevButton.disabled = adminPageState.loadingUsers || adminPageState.page <= 1;
    nextButton.disabled = adminPageState.loadingUsers || adminPageState.page >= totalPages || adminPageState.total === 0;
}

function renderAdminUsers() {
    const container = document.getElementById("admin-users-list");
    if (!container) {
        return;
    }

    if (!adminPageState.users.length) {
        container.innerHTML = '<p class="moderation-dashboard-empty">Žádné účty neodpovídají aktuálním filtrům.</p>';
        renderAdminUsersSummary();
        return;
    }

    container.innerHTML = adminPageState.users.map((user) => {
        const avatarURL = user.picture || DEFAULT_AVATAR_URL;
        const profileURL = buildPublicProfileURL(user.id);
        const restrictionBadges = adminRestrictionBadges(user);
        const lastModeration = user.moderated_at ? `Poslední interní zásah: ${formatOptionalDateTime(user.moderated_at)}` : "Bez interní poznámky";

        return `
            <article class="admin-user-card">
                <div class="admin-user-head">
                    <div class="admin-user-identity">
                        <img src="${escapeHtml(avatarURL)}" alt="${escapeHtml(user.preferred_username || "Uživatel")}" class="admin-user-avatar" loading="lazy">
                        <div>
                            <h3>${escapeHtml(user.preferred_username || "Bez jména")}</h3>
                            <p class="muted-copy">#${escapeHtml(String(user.id))}${user.email ? ` · ${escapeHtml(user.email)}` : ""}</p>
                        </div>
                    </div>
                    <div class="admin-user-actions">
                        <a href="${escapeHtml(profileURL)}" class="btn btn-secondary">Profil</a>
                    </div>
                </div>

                <div class="moderation-item-meta">
                    <span>${user.public_posts_count || 0} publikací</span>
                    <span>${user.public_captures_count || 0} veřejných fotografií</span>
                </div>

                <div class="admin-user-badges">
                    ${adminRoleBadges(user)}
                    ${restrictionBadges}
                </div>

                <p class="muted-copy">${escapeHtml(lastModeration)}</p>
                ${user.moderation_note ? `
                    <div class="moderation-context-box">
                        <p class="muted-copy">Aktuální interní poznámka</p>
                        <p>${escapeHtml(user.moderation_note)}</p>
                    </div>
                ` : ""}
            </article>
        `;
    }).join("");

    renderAdminUsersSummary();
}

function formatBackupSize(sizeBytes) {
    const value = Number(sizeBytes || 0);
    if (value <= 0) {
        return "0 B";
    }
    if (value < 1024) {
        return `${value} B`;
    }
    if (value < 1024 * 1024) {
        return `${(value / 1024).toFixed(1)} KB`;
    }
    return `${(value / (1024 * 1024)).toFixed(2)} MB`;
}

function backupTriggerLabel(triggerKind) {
    return triggerKind === "scheduled" ? "Plánovaná" : "Ruční";
}

function backupStatusLabel(status) {
    switch (status) {
        case "completed":
            return "Dokončeno";
        case "failed":
            return "Selhalo";
        case "running":
            return "Běží";
        default:
            return status || "Neznámý stav";
    }
}

function renderAdminBackups() {
    const listNode = document.getElementById("admin-backups-list");
    const summaryNode = document.getElementById("admin-backups-summary");
    const loadMoreButton = document.getElementById("admin-backups-load-more");
    if (!listNode || !summaryNode || !loadMoreButton) {
        return;
    }

    const state = adminPageState.backups;
    if (state.loading && state.items.length === 0) {
        summaryNode.textContent = "Načítám historii záloh...";
        listNode.innerHTML = '<p class="moderation-dashboard-empty">Načítám historii záloh...</p>';
        loadMoreButton.style.display = "none";
        return;
    }

    if (!state.items.length) {
        summaryNode.textContent = "Zatím není uložená žádná záloha.";
        listNode.innerHTML = '<p class="moderation-dashboard-empty">Zatím není uložená žádná záloha.</p>';
        loadMoreButton.style.display = "none";
        return;
    }

    summaryNode.textContent = `Načteno ${state.items.length} z ${state.total} záloh.`;
    listNode.innerHTML = state.items.map((backup) => {
        const meta = [
            `${backupTriggerLabel(backup.trigger_kind)} záloha`,
            backup.initiated_by_name ? `Spustil: ${backup.initiated_by_name}` : "Spustil systém",
            backup.started_at ? `Start: ${formatDateTime(backup.started_at)}` : "",
            backup.finished_at ? `Konec: ${formatDateTime(backup.finished_at)}` : "",
            backup.size_bytes ? `Velikost: ${formatBackupSize(backup.size_bytes)}` : ""
        ].filter(Boolean);

        return `
            <article class="moderation-item-card">
                <div class="moderation-item-head">
                    <div>
                        <h3>${escapeHtml(backupStatusLabel(backup.status))}</h3>
                        <p class="muted-copy">${escapeHtml(meta.join(" · "))}</p>
                    </div>
                    <div class="admin-user-badges">
                        <span class="capture-access-badge">${escapeHtml(backupTriggerLabel(backup.trigger_kind))}</span>
                        ${backup.status === "completed" ? '<span class="capture-access-badge capture-access-badge-free">Uloženo</span>' : ""}
                        ${backup.status === "failed" ? '<span class="capture-access-badge capture-access-badge-paid">Chyba</span>' : ""}
                    </div>
                </div>
                ${backup.storage_key ? `
                    <div class="moderation-context-box">
                        <p class="muted-copy">Private storage key</p>
                        <p>${escapeHtml(backup.storage_key)}</p>
                    </div>
                ` : ""}
                ${backup.checksum_sha256 ? `<p class="muted-copy">SHA-256: ${escapeHtml(backup.checksum_sha256)}</p>` : ""}
                ${backup.error_message ? `
                    <div class="moderation-context-box">
                        <p class="muted-copy">Chybová hláška</p>
                        <p>${escapeHtml(backup.error_message)}</p>
                    </div>
                ` : ""}
            </article>
        `;
    }).join("");

    loadMoreButton.style.display = state.hasMore ? "inline-flex" : "none";
    loadMoreButton.disabled = state.loading;
}

async function loadAdminOverview() {
    const payload = await apiJsonRequest("/api/admin/overview");
    adminPageState.overview = payload?.overview || null;
    renderAdminOverview();
}

function buildAdminUsersPath() {
    const offset = (adminPageState.page - 1) * adminPageState.pageSize;
    const params = new URLSearchParams({
        limit: String(adminPageState.pageSize),
        offset: String(offset)
    });
    if (adminPageState.filters.query) {
        params.set("query", adminPageState.filters.query);
    }
    if (adminPageState.filters.role) {
        params.set("role", adminPageState.filters.role);
    }
    if (adminPageState.filters.status) {
        params.set("status", adminPageState.filters.status);
    }
    return `/api/admin/users?${params.toString()}`;
}

async function loadAdminUsers() {
    adminPageState.loadingUsers = true;
    renderAdminUsersSummary();

    try {
        const payload = await apiJsonRequest(buildAdminUsersPath());
        adminPageState.users = Array.isArray(payload?.users) ? payload.users : [];
        adminPageState.total = Number(payload?.total || 0);
    } finally {
        adminPageState.loadingUsers = false;
        renderAdminUsers();
    }
}

async function loadAdminBackups({ append = false } = {}) {
    const state = adminPageState.backups;
    if (state.loading) {
        return;
    }

    if (!append) {
        state.items = [];
        state.offset = 0;
        state.total = 0;
        state.hasMore = false;
    }

    state.loading = true;
    renderAdminBackups();
    try {
        const payload = await apiJsonRequest(`/api/admin/backups?limit=${state.limit}&offset=${state.offset}`);
        const items = Array.isArray(payload?.backups) ? payload.backups : [];
        state.items = append ? state.items.concat(items) : items;
        state.offset = state.items.length;
        state.total = Number(payload?.total || 0);
        state.hasMore = Boolean(payload?.has_more);
    } finally {
        state.loading = false;
        renderAdminBackups();
    }
}

async function runAdminBackup() {
    const statusNode = document.getElementById("admin-backup-status");
    const button = document.getElementById("admin-run-backup");
    if (adminPageState.backups.running) {
        return;
    }

    adminPageState.backups.running = true;
    if (button) {
        button.disabled = true;
    }
    setStatusMessage(statusNode, "Spouštím zálohu databáze...");

    try {
        const payload = await apiJsonRequest("/api/admin/backups/run", { method: "POST" });
        const backup = payload?.backup || null;
        setStatusMessage(
            statusNode,
            backup?.storage_key
                ? `Záloha dokončena. Uloženo do ${backup.storage_key}.`
                : "Záloha dokončena.",
            "success"
        );
        await loadAdminBackups({ append: false });
        await loadAdminOverview();
    } catch (error) {
        console.error("Failed to run admin backup", error);
        setStatusMessage(statusNode, error.message || "Nepodařilo se spustit zálohu.", "error");
    } finally {
        adminPageState.backups.running = false;
        if (button) {
            button.disabled = false;
        }
    }
}

function showAdminPageError(message) {
    const errorNode = document.getElementById("admin-page-error");
    const dashboard = document.getElementById("admin-dashboard");
    if (dashboard) {
        dashboard.hidden = true;
    }
    if (errorNode) {
        errorNode.hidden = false;
        errorNode.textContent = message;
    }
}

function attachAdminPageEvents() {
    const filtersForm = document.getElementById("admin-users-filters");
    const resetButton = document.getElementById("admin-filters-reset");
    const prevButton = document.getElementById("admin-users-prev");
    const nextButton = document.getElementById("admin-users-next");
    const runBackupButton = document.getElementById("admin-run-backup");
    const loadMoreBackupsButton = document.getElementById("admin-backups-load-more");

    if (filtersForm) {
        filtersForm.addEventListener("submit", async (event) => {
            event.preventDefault();
            adminPageState.page = 1;
            adminPageState.filters.query = String(document.getElementById("admin-filter-query")?.value || "").trim();
            adminPageState.filters.role = String(document.getElementById("admin-filter-role")?.value || "").trim();
            adminPageState.filters.status = String(document.getElementById("admin-filter-status")?.value || "").trim();
            await loadAdminUsers();
        });
    }

    if (resetButton) {
        resetButton.addEventListener("click", async () => {
            adminPageState.page = 1;
            adminPageState.filters = { query: "", role: "", status: "" };
            const queryInput = document.getElementById("admin-filter-query");
            const roleSelect = document.getElementById("admin-filter-role");
            const statusSelect = document.getElementById("admin-filter-status");
            if (queryInput) queryInput.value = "";
            if (roleSelect) roleSelect.value = "";
            if (statusSelect) statusSelect.value = "";
            await loadAdminUsers();
        });
    }

    if (prevButton) {
        prevButton.addEventListener("click", async () => {
            if (adminPageState.page <= 1) {
                return;
            }
            adminPageState.page -= 1;
            await loadAdminUsers();
        });
    }

    if (nextButton) {
        nextButton.addEventListener("click", async () => {
            const totalPages = Math.max(1, Math.ceil(adminPageState.total / adminPageState.pageSize));
            if (adminPageState.page >= totalPages) {
                return;
            }
            adminPageState.page += 1;
            await loadAdminUsers();
        });
    }

    if (runBackupButton) {
        runBackupButton.addEventListener("click", runAdminBackup);
    }

    if (loadMoreBackupsButton) {
        loadMoreBackupsButton.addEventListener("click", () => loadAdminBackups({ append: true }));
    }
}

async function initAdminPage() {
    const session = await apiGet("/api/session");
    if (!session || !session.logged_in) {
        window.location.href = "/";
        return;
    }

    const me = await apiGet("/api/me");
    if (!me) {
        showAdminPageError("Nepodařilo se načíst vaši identitu.");
        return;
    }

    setAppIdentity(session, me);
    renderHeader(session, me);
    adminPageState.session = session;
    adminPageState.me = me;

    if (!userCanAdminClient(me)) {
        setText("admin-page-note", "Tato stránka je dostupná jen pro administrátory.");
        showAdminPageError("Nemáte oprávnění pro administraci.");
        return;
    }

    setText("admin-page-note", "Admin účet je pevně vyhrazený pro Houbamzdar. Tady je přehled účtů a omezení; zálohy databáze připojíme v dalším kroku.");
    const dashboard = document.getElementById("admin-dashboard");
    if (dashboard) {
        dashboard.hidden = false;
    }

    attachAdminPageEvents();

    try {
        await Promise.all([loadAdminOverview(), loadAdminUsers(), loadAdminBackups({ append: false })]);
    } catch (error) {
        console.error("Failed to initialize admin page", error);
        showAdminPageError(error.message || "Administraci se nepodařilo načíst.");
    }
}

document.addEventListener("DOMContentLoaded", initAdminPage);
