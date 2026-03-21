const moderationDashboardState = {
    session: null,
    me: null,
    captures: {
        items: [],
        limit: 12,
        offset: 0,
        total: 0,
        hasMore: false,
        loading: false
    },
    posts: {
        items: [],
        limit: 8,
        offset: 0,
        total: 0,
        hasMore: false,
        loading: false
    },
    comments: {
        items: [],
        limit: 12,
        offset: 0,
        total: 0,
        hasMore: false,
        loading: false
    }
};

function moderationExcerpt(text, maxLength = 220) {
    const value = String(text || "").trim().replace(/\s+/g, " ");
    if (!value) {
        return "";
    }
    if (value.length <= maxLength) {
        return value;
    }
    return `${value.slice(0, maxLength - 1).trimEnd()}…`;
}

function moderationActorLabel(item) {
    const name = String(item?.moderated_by_name || "").trim();
    if (name) {
        return name;
    }
    const id = Number(item?.moderated_by_user_id || 0);
    return id > 0 ? `#${id}` : "neznámý moderátor";
}

function moderationReasonLabel(item) {
    const reason = String(item?.moderation_reason_code || "").trim();
    return reason ? `Důvod: ${reason}` : "Důvod nebyl uložen";
}

function moderationNoteHtml(item) {
    const note = String(item?.moderation_note || "").trim();
    if (!note) {
        return "";
    }
    return `
        <div class="moderation-context-box">
            <p class="muted-copy">Poznámka moderátora</p>
            <p>${escapeHtml(note)}</p>
        </div>
    `;
}

function updateModerationSectionSummary(key) {
    const summary = document.getElementById(`moderation-hidden-${key}-summary`);
    const loadMoreButton = document.getElementById(`moderation-hidden-${key}-load-more`);
    const sectionState = moderationDashboardState[key];
    if (!summary || !sectionState) {
        return;
    }

    if (sectionState.loading && sectionState.items.length === 0) {
        summary.textContent = "Načítám...";
    } else if (sectionState.total === 0) {
        summary.textContent = "Nic není skryto.";
    } else {
        summary.textContent = `Načteno ${sectionState.items.length} z ${sectionState.total}.`;
    }

    if (loadMoreButton) {
        loadMoreButton.style.display = sectionState.hasMore ? "inline-flex" : "none";
        loadMoreButton.disabled = sectionState.loading;
    }
}

function renderHiddenCaptures() {
    const container = document.getElementById("moderation-hidden-captures");
    if (!container) {
        return;
    }

    const items = moderationDashboardState.captures.items;
    if (!items.length) {
        container.innerHTML = '<p class="moderation-dashboard-empty">Žádné skryté fotografie.</p>';
        updateModerationSectionSummary("captures");
        return;
    }

    container.innerHTML = items.map((capture) => {
        const previewHtml = capture.public_url
            ? buildCaptureImageTag(capture, {
                variant: "thumb",
                alt: buildCaptureSpeciesLabel(capture) || "Skrytá fotografie",
                loading: "lazy",
                sizes: "(max-width: 720px) 50vw, 240px"
            })
            : "";
        const authorURL = buildPublicProfileURL(capture.author_user_id);
        const speciesLabel = buildCaptureSpeciesLabel(capture) || moderationExcerpt(capture.original_file_name, 80) || "Skrytá fotografie";
        const moderationMeta = [
            `Skryl: ${moderationActorLabel(capture)}`,
            moderationReasonLabel(capture),
            capture.moderated_at ? `Zásah: ${formatDateTime(capture.moderated_at)}` : ""
        ].filter(Boolean);

        return `
            <article class="moderation-item-card">
                <div class="moderation-capture-layout">
                    ${previewHtml}
                    <div class="moderation-item-copy">
                        <div class="moderation-item-head">
                            <div>
                                <h3>${escapeHtml(speciesLabel)}</h3>
                                <p class="muted-copy">
                                    Autor: <a href="${escapeHtml(authorURL)}">${escapeHtml(capture.author_name || "Neznámý houbař")}</a>
                                </p>
                            </div>
                            <div class="moderation-item-actions">
                                <button type="button" class="btn btn-secondary moderation-restore-btn" data-kind="capture" data-id="${escapeHtml(capture.id)}">Obnovit</button>
                            </div>
                        </div>
                        <div class="moderation-item-meta">
                            <span>${escapeHtml(formatDateTime(capture.published_at || capture.captured_at))}</span>
                            ${capture.kraj_name ? `<span>Kraj: ${escapeHtml(capture.kraj_name)}</span>` : ""}
                        </div>
                        <p class="muted-copy">${escapeHtml(moderationMeta.join(" · "))}</p>
                        ${moderationNoteHtml(capture)}
                    </div>
                </div>
            </article>
        `;
    }).join("");

    updateModerationSectionSummary("captures");
}

function renderHiddenPosts() {
    const container = document.getElementById("moderation-hidden-posts");
    if (!container) {
        return;
    }

    const items = moderationDashboardState.posts.items;
    if (!items.length) {
        container.innerHTML = '<p class="moderation-dashboard-empty">Žádné skryté příspěvky.</p>';
        updateModerationSectionSummary("posts");
        return;
    }

    container.innerHTML = items.map((post) => {
        const authorURL = buildPublicProfileURL(post.author_user_id);
        const moderationMeta = [
            `Skryl: ${moderationActorLabel(post)}`,
            moderationReasonLabel(post),
            post.moderated_at ? `Zásah: ${formatDateTime(post.moderated_at)}` : ""
        ].filter(Boolean);
        const previews = Array.isArray(post.captures) ? post.captures.slice(0, 4) : [];

        return `
            <article class="moderation-item-card">
                <div class="moderation-item-head">
                    <div>
                        <h3>Skrytá publikace</h3>
                        <p class="muted-copy">
                            Autor: <a href="${escapeHtml(authorURL)}">${escapeHtml(post.author_name || "Neznámý houbař")}</a>
                        </p>
                    </div>
                    <div class="moderation-item-actions">
                        <button type="button" class="btn btn-secondary moderation-restore-btn" data-kind="post" data-id="${escapeHtml(post.id)}">Obnovit</button>
                    </div>
                </div>
                <div class="moderation-item-meta">
                    <span>${escapeHtml(formatDateTime(post.created_at))}</span>
                    ${post.updated_at && post.updated_at !== post.created_at ? `<span>Upraveno: ${escapeHtml(formatDateTime(post.updated_at))}</span>` : ""}
                </div>
                <div class="moderation-item-copy">
                    <p>${escapeHtml(moderationExcerpt(post.content, 420))}</p>
                    <p class="muted-copy">${escapeHtml(moderationMeta.join(" · "))}</p>
                </div>
                ${moderationNoteHtml(post)}
                ${previews.length ? `
                    <div class="moderation-preview-grid">
                        ${previews.map((capture) => {
                            const previewHtml = capture.public_url
                                ? buildCaptureImageTag(capture, {
                                    variant: "thumb",
                                    alt: "Náhled publikace",
                                    loading: "lazy",
                                    sizes: "(max-width: 720px) 33vw, 180px"
                                })
                                : "";
                            if (!previewHtml) {
                                return "";
                            }
                            return previewHtml;
                        }).join("")}
                    </div>
                ` : ""}
            </article>
        `;
    }).join("");

    updateModerationSectionSummary("posts");
}

function renderHiddenComments() {
    const container = document.getElementById("moderation-hidden-comments");
    if (!container) {
        return;
    }

    const items = moderationDashboardState.comments.items;
    if (!items.length) {
        container.innerHTML = '<p class="moderation-dashboard-empty">Žádné skryté komentáře.</p>';
        updateModerationSectionSummary("comments");
        return;
    }

    container.innerHTML = items.map((comment) => {
        const authorURL = buildPublicProfileURL(comment.author_user_id);
        const postAuthorURL = buildPublicProfileURL(comment.post_author_user_id);
        const moderationMeta = [
            `Skryl: ${moderationActorLabel(comment)}`,
            moderationReasonLabel(comment),
            comment.moderated_at ? `Zásah: ${formatDateTime(comment.moderated_at)}` : ""
        ].filter(Boolean);

        return `
            <article class="moderation-item-card">
                <div class="moderation-item-head">
                    <div>
                        <h3>Skrytý komentář</h3>
                        <p class="muted-copy">
                            Autor: <a href="${escapeHtml(authorURL)}">${escapeHtml(comment.author_name || "Neznámý houbař")}</a>
                        </p>
                    </div>
                    <div class="moderation-item-actions">
                        <button type="button" class="btn btn-secondary moderation-restore-btn" data-kind="comment" data-id="${escapeHtml(comment.id)}">Obnovit</button>
                    </div>
                </div>
                <div class="moderation-item-meta">
                    <span>${escapeHtml(formatDateTime(comment.created_at))}</span>
                    ${comment.updated_at && comment.updated_at !== comment.created_at ? `<span>Upraveno: ${escapeHtml(formatDateTime(comment.updated_at))}</span>` : ""}
                </div>
                <div class="moderation-item-copy">
                    <p>${escapeHtml(moderationExcerpt(comment.content, 340))}</p>
                    <p class="muted-copy">${escapeHtml(moderationMeta.join(" · "))}</p>
                </div>
                ${moderationNoteHtml(comment)}
                <div class="moderation-context-box">
                    <p class="muted-copy">
                        Pod publikací autora <a href="${escapeHtml(postAuthorURL)}">${escapeHtml(comment.post_author_name || "Neznámý houbař")}</a>
                    </p>
                    <p>${escapeHtml(moderationExcerpt(comment.post_content, 260) || "Obsah původní publikace není k dispozici.")}</p>
                </div>
            </article>
        `;
    }).join("");

    updateModerationSectionSummary("comments");
}

async function loadHiddenSection(key, { reset = false } = {}) {
    const sectionState = moderationDashboardState[key];
    if (!sectionState || sectionState.loading) {
        return;
    }

    if (reset) {
        sectionState.items = [];
        sectionState.offset = 0;
        sectionState.total = 0;
        sectionState.hasMore = false;
    }

    sectionState.loading = true;
    updateModerationSectionSummary(key);

    try {
        const pathMap = {
            captures: "/api/moderation/hidden-captures",
            posts: "/api/moderation/hidden-posts",
            comments: "/api/moderation/hidden-comments"
        };
        const payload = await apiJsonRequest(`${pathMap[key]}?limit=${sectionState.limit}&offset=${sectionState.offset}`);
        const itemKeyMap = {
            captures: "captures",
            posts: "posts",
            comments: "comments"
        };
        const items = Array.isArray(payload?.[itemKeyMap[key]]) ? payload[itemKeyMap[key]] : [];
        sectionState.items = sectionState.items.concat(items);
        sectionState.offset += items.length;
        sectionState.total = Number(payload?.total || 0);
        sectionState.hasMore = Boolean(payload?.has_more);
    } catch (error) {
        console.error(`Failed to load moderation ${key}`, error);
        const container = document.getElementById(`moderation-hidden-${key}`);
        if (container && sectionState.items.length === 0) {
            container.innerHTML = `<p class="moderation-dashboard-empty">${escapeHtml(error.message || "Nepodařilo se načíst obsah.")}</p>`;
        }
    } finally {
        sectionState.loading = false;
        if (key === "captures") renderHiddenCaptures();
        if (key === "posts") renderHiddenPosts();
        if (key === "comments") renderHiddenComments();
    }
}

async function restoreModerationItem(kind, id, button) {
    const routeMap = {
        capture: `/api/moderation/captures/${encodeURIComponent(id)}/visibility`,
        post: `/api/moderation/posts/${encodeURIComponent(id)}/visibility`,
        comment: `/api/moderation/comments/${encodeURIComponent(id)}/visibility`
    };
    const sectionMap = {
        capture: "captures",
        post: "posts",
        comment: "comments"
    };
    if (!routeMap[kind] || !sectionMap[kind]) {
        return;
    }

    const note = window.prompt("Poznámka k obnovení (volitelné):", "");
    if (note === null) {
        return;
    }

    if (button) {
        button.disabled = true;
    }

    try {
        await apiJsonRequest(routeMap[kind], {
            method: "POST",
            body: {
                hidden: false,
                reason_code: "moderator_restore",
                note
            }
        });
        await loadHiddenSection(sectionMap[kind], { reset: true });
    } catch (error) {
        console.error("Failed to restore moderation item", error);
        window.alert(error.message || "Obsah se nepodařilo obnovit.");
    } finally {
        if (button) {
            button.disabled = false;
        }
    }
}

function attachModerationDashboardHandlers() {
    document.querySelectorAll(".moderation-restore-btn").forEach((button) => {
        if (button.dataset.bound === "1") {
            return;
        }
        button.dataset.bound = "1";
        button.addEventListener("click", async () => {
            await restoreModerationItem(button.dataset.kind, button.dataset.id, button);
        });
    });

    [
        ["captures", "moderation-hidden-captures-load-more"],
        ["posts", "moderation-hidden-posts-load-more"],
        ["comments", "moderation-hidden-comments-load-more"]
    ].forEach(([key, buttonID]) => {
        const button = document.getElementById(buttonID);
        if (!button || button.dataset.bound === "1") {
            return;
        }
        button.dataset.bound = "1";
        button.addEventListener("click", async () => {
            await loadHiddenSection(key, { reset: false });
        });
    });
}

function showModerationPageError(message) {
    const errorNode = document.getElementById("moderation-page-error");
    const dashboard = document.getElementById("moderation-dashboard");
    const note = document.getElementById("moderation-page-note");

    if (note) {
        note.textContent = "";
    }
    if (dashboard) {
        dashboard.hidden = true;
    }
    if (errorNode) {
        errorNode.hidden = false;
        errorNode.innerHTML = `<p class="muted-copy">${escapeHtml(message)}</p>`;
    }
}

async function initModerationPage() {
    if (document.body.dataset.page !== "moderation") {
        return;
    }

    moderationDashboardState.session = await apiGet("/api/session");
    if (moderationDashboardState.session?.logged_in) {
        moderationDashboardState.me = await apiGet("/api/me");
    }

    setAppIdentity(moderationDashboardState.session, moderationDashboardState.me);
    renderHeader(moderationDashboardState.session, moderationDashboardState.me);

    if (!moderationDashboardState.session?.logged_in) {
        showModerationPageError("Moderátorský panel je dostupný jen přihlášeným uživatelům.");
        return;
    }
    if (!userCanModerateClient(moderationDashboardState.me)) {
        showModerationPageError("Tento účet nemá oprávnění otevřít moderátorský panel.");
        return;
    }

    const dashboard = document.getElementById("moderation-dashboard");
    const errorNode = document.getElementById("moderation-page-error");
    const note = document.getElementById("moderation-page-note");
    if (dashboard) {
        dashboard.hidden = false;
    }
    if (errorNode) {
        errorNode.hidden = true;
    }
    if (note) {
        note.textContent = "Obnovujte jen obsah, u kterého je jasné, že šlo o chybný zásah nebo už pominul důvod skrytí.";
    }

    attachModerationDashboardHandlers();
    await Promise.all([
        loadHiddenSection("captures", { reset: true }),
        loadHiddenSection("posts", { reset: true }),
        loadHiddenSection("comments", { reset: true })
    ]);
    attachModerationDashboardHandlers();
}

document.addEventListener("DOMContentLoaded", initModerationPage);
