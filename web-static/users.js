const usersState = {
    page: 1,
    pageSize: 48,
    hasMore: true,
    isLoading: false,
    query: "",
    sort: "popularity",
    users: []
};

function readUsersFiltersFromQuery() {
    const params = new URLSearchParams(window.location.search);
    usersState.query = params.get("q") || "";
    const sortParam = params.get("sort");
    if (["popularity", "posts", "captures", "comments"].includes(sortParam)) {
        usersState.sort = sortParam;
    } else {
        usersState.sort = "popularity";
    }
}

function syncUsersFilterInputs() {
    const queryInput = document.getElementById("users-filter-query");
    const sortSelect = document.getElementById("users-filter-sort");
    
    if (queryInput) queryInput.value = usersState.query;
    if (sortSelect) sortSelect.value = usersState.sort;
}

function syncUsersQueryString() {
    const params = new URLSearchParams();
    if (usersState.query) params.set("q", usersState.query);
    if (usersState.sort !== "popularity") params.set("sort", usersState.sort);

    const qs = params.toString();
    const nextURL = qs ? `${window.location.pathname}?${qs}` : window.location.pathname;
    window.history.replaceState({}, "", nextURL);
}

function buildUsersQuery() {
    const params = new URLSearchParams();
    params.set("limit", String(usersState.pageSize));
    params.set("offset", String((usersState.page - 1) * usersState.pageSize));
    if (usersState.query) params.set("q", usersState.query);
    params.set("sort", usersState.sort);
    return params.toString();
}

async function loadUsersPage(reset = false) {
    if (usersState.isLoading) return;

    if (reset) {
        usersState.page = 1;
        usersState.hasMore = true;
        usersState.users = [];
        const container = document.getElementById("users-list");
        if (container) container.innerHTML = "";
    }

    usersState.isLoading = true;
    updateUsersSummary();
    const loadMoreBtn = document.getElementById("users-load-more-btn");
    if (loadMoreBtn) loadMoreBtn.disabled = true;

    try {
        const result = await apiGet(`/api/public/users?${buildUsersQuery()}`);
        if (!result || !result.ok || !Array.isArray(result.users)) {
            throw new Error("Nepodařilo se načíst houbaře.");
        }

        usersState.users = usersState.users.concat(result.users);
        usersState.hasMore = result.has_more !== false && result.users.length === usersState.pageSize;
        usersState.page += 1;

        renderUsers(result.users);
    } catch (error) {
        console.error("Failed to load users", error);
        const summary = document.getElementById("users-summary");
        if (summary && usersState.users.length === 0) {
            summary.textContent = "Chyba při načítání houbařů.";
        }
    } finally {
        usersState.isLoading = false;
        updateUsersSummary();
        const loadMoreRow = document.getElementById("users-load-more");
        if (loadMoreRow) {
            loadMoreRow.hidden = !usersState.hasMore;
        }
        if (loadMoreBtn) loadMoreBtn.disabled = false;
    }
}

function updateUsersSummary() {
    const summary = document.getElementById("users-summary");
    if (!summary) return;

    if (usersState.isLoading && usersState.users.length === 0) {
        summary.textContent = "Hledám houbaře...";
    } else if (usersState.users.length === 0) {
        summary.textContent = "Žádní houbaři nebyli nalezeni.";
    } else {
        summary.textContent = `Nalezeno ${usersState.users.length} houbařů.`;
    }
}

function renderUsers(users) {
    const container = document.getElementById("users-list");
    if (!container) return;

    const html = users.map(user => {
        const avatarHtml = buildAvatarImageHtml(user.picture, user.preferred_username, "user-card-avatar");
        
        let statsHtml = "";
        if (usersState.sort === "posts") {
            statsHtml = `<span>📝 ${user.public_posts_count} publikací</span>`;
        } else if (usersState.sort === "captures") {
            statsHtml = `<span>📸 ${user.public_captures_count} fotek</span>`;
        } else if (usersState.sort === "comments") {
            statsHtml = `<span>💬 ${user.public_comments_count || 0} komentářů</span>`; // Using the struct mapped value if exists
        } else {
            statsHtml = `<span>👥 ${user.followers_count} sledujících</span>`;
        }

        return `
            <a href="${buildPublicProfileURL(user.id)}" class="user-card card">
                ${avatarHtml}
                <h3 class="user-card-name">${escapeHtml(user.preferred_username)}</h3>
                <div class="user-card-stats">${statsHtml}</div>
            </a>
        `;
    }).join("");

    container.insertAdjacentHTML("beforeend", html);
}

document.addEventListener("DOMContentLoaded", async () => {
    if (document.body.dataset.page !== "users") return;

    const session = await apiGet("/api/session");
    let me = null;
    if (session && session.logged_in) {
        me = await apiGet("/api/me");
    }
    setAppIdentity(session, me);
    renderHeader(session, me);

    readUsersFiltersFromQuery();
    syncUsersFilterInputs();

    const filterForm = document.getElementById("users-filter-form");
    if (filterForm) {
        filterForm.addEventListener("submit", (e) => {
            e.preventDefault();
            usersState.query = (document.getElementById("users-filter-query")?.value || "").trim();
            usersState.sort = document.getElementById("users-filter-sort")?.value || "popularity";
            syncUsersQueryString();
            loadUsersPage(true);
        });
    }

    const resetBtn = document.getElementById("users-filter-reset");
    if (resetBtn) {
        resetBtn.addEventListener("click", () => {
            usersState.query = "";
            usersState.sort = "popularity";
            syncUsersFilterInputs();
            syncUsersQueryString();
            loadUsersPage(true);
        });
    }

    const loadMoreBtn = document.getElementById("users-load-more-btn");
    if (loadMoreBtn) {
        loadMoreBtn.addEventListener("click", () => loadUsersPage(false));
    }

    loadUsersPage(true);
});
