const state = {
    posts: [],
    page: 1,
    pageSize: 10,
    hasMore: true,
    session: null,
    me: null,
    sort: "latest_desc"
};

const FEED_SORT_VALUES = new Set([
    "latest_desc",
    "likes_desc",
    "comments_desc"
]);

function activeSession() {
    return state.session || window.appSession || null;
}

function activeUser() {
    return state.me || window.appMe || null;
}

function normalizeFeedSort(value) {
    return FEED_SORT_VALUES.has(value) ? value : "latest_desc";
}

function getPostCreatedTime(post) {
    const timestamp = Date.parse(post?.created_at || "");
    return Number.isNaN(timestamp) ? 0 : timestamp;
}

function getPostLikesCount(post) {
    return Number(post?.likes_count || 0);
}

function getPostCommentsCount(post) {
    if (typeof post?.comments_count === "number") {
        return post.comments_count;
    }
    return Array.isArray(post?.comments) ? post.comments.length : 0;
}

function sortFeedPosts(posts, sortValue = state.sort) {
    const sort = normalizeFeedSort(sortValue);
    return [...(posts || [])].sort((left, right) => {
        if (sort === "likes_desc") {
            const likeDiff = getPostLikesCount(right) - getPostLikesCount(left);
            if (likeDiff !== 0) {
                return likeDiff;
            }
        }

        if (sort === "comments_desc") {
            const commentDiff = getPostCommentsCount(right) - getPostCommentsCount(left);
            if (commentDiff !== 0) {
                return commentDiff;
            }
        }

        return getPostCreatedTime(right) - getPostCreatedTime(left);
    });
}

function renderFeedState() {
    const container = document.getElementById("feed-container");
    if (!container) {
        return;
    }

    if (!state.posts.length) {
        container.innerHTML = '<p class="muted-copy feed-list-status">Zatím nejsou žádné příspěvky k zobrazení.</p>';
        return;
    }

    container.innerHTML = "";
    renderPosts(sortFeedPosts(state.posts), container, {
        postsStore: state.posts,
        onPostDeleted: () => {
            renderFeedState();
        }
    });
}

function applyFeedSort(sortValue, options = {}) {
    state.sort = normalizeFeedSort(sortValue);

    const select = document.getElementById("feed-sort-select");
    if (select) {
        select.value = state.sort;
    }

    renderFeedState();

    if (options.closeOnMobile) {
        const panel = document.getElementById("feed-sort-panel");
        if (panel && window.matchMedia("(max-width: 768px)").matches) {
            panel.open = false;
        }
    }
}

function findPostById(postID, postsStore = state.posts) {
    return (postsStore || []).find((post) => post.id === postID) || null;
}

function formatCommentCount(count) {
    if (count === 1) return "1 komentář";
    if (count >= 2 && count <= 4) return `${count} komentáře`;
    return `${count} komentářů`;
}

function commentIsMine(comment) {
    const me = activeUser();
    return Boolean(me && comment && Number(comment.author_user_id) === Number(me.id));
}

function commentCanBeModerated() {
    return userCanModerateClient(activeUser());
}

function buildAuthorLinkHTML(userID, avatarURL, authorName, avatarClass, nameClass) {
    const safeAvatar = escapeHtml(avatarURL || DEFAULT_AVATAR_URL);
    const safeName = escapeHtml(authorName || "Registrovaný houbař");
    const href = buildPublicProfileURL(userID);

    return `
        <a href="${escapeHtml(href)}" class="author-link">
            <img src="${safeAvatar}" alt="${safeName}" class="${avatarClass}">
            <span class="${nameClass}">${safeName}</span>
        </a>
    `;
}

function buildCommentActionsHtml(comment) {
    const actions = [];

    if (commentIsMine(comment)) {
        actions.push(`
            <button type="button" class="btn btn-secondary comment-action-btn comment-edit-btn" data-comment-id="${escapeHtml(comment.id)}">Upravit</button>
            <button type="button" class="btn btn-secondary comment-action-btn comment-delete-btn" data-comment-id="${escapeHtml(comment.id)}">Smazat</button>
        `);
    }

    if (commentCanBeModerated()) {
        actions.push(`
            <button type="button" class="btn btn-secondary comment-action-btn comment-hide-btn" data-comment-id="${escapeHtml(comment.id)}">Skrýt</button>
        `);
    }

    if (!actions.length) {
        return "";
    }

    return `
        <div class="comment-actions">
            ${actions.join("")}
        </div>
    `;
}

function buildCommentsListHtml(post) {
    const comments = Array.isArray(post.comments) ? post.comments : [];
    if (!comments.length) {
        return '<p class="comment-empty">Zatím tu není žádný komentář.</p>';
    }

    return comments.map((comment) => {
        const avatarUrl = comment.author_avatar || DEFAULT_AVATAR_URL;
        const authorName = comment.author_name || "Registrovaný houbař";
        const content = escapeHtml(comment.content || "").replace(/\n/g, "<br>");
        const mine = commentIsMine(comment);
        const edited = comment.updated_at && comment.created_at && comment.updated_at !== comment.created_at
            ? '<span class="comment-edited">upraveno</span>'
            : "";
        const editFormHtml = mine
            ? `
                    <form class="comment-edit-form" data-comment-id="${escapeHtml(comment.id)}" hidden>
                        <textarea class="comment-input comment-edit-input" maxlength="1000" required>${escapeHtml(comment.content || "")}</textarea>
                        <div class="comment-form-row">
                            <button type="button" class="btn btn-secondary comment-cancel-btn">Zrušit</button>
                            <button type="submit" class="btn btn-primary comment-save-btn">Uložit</button>
                        </div>
                        <p class="status-message comment-status" aria-live="polite"></p>
                    </form>
                `
            : "";

        return `
            <article class="comment-item" data-comment-id="${escapeHtml(comment.id)}">
                ${buildAuthorLinkHTML(comment.author_user_id, avatarUrl, authorName, "comment-avatar", "comment-author")}
                <div class="comment-body">
                    <div class="comment-meta">
                        <div class="comment-meta-copy">
                            <span class="comment-date">${escapeHtml(formatDateTime(comment.created_at))}</span>
                            ${edited}
                        </div>
                        ${buildCommentActionsHtml(comment)}
                    </div>
                    <div class="comment-text">${content}</div>
                    ${editFormHtml}
                </div>
            </article>
        `;
    }).join("");
}

function buildCommentsSectionHtml(post) {
    const comments = Array.isArray(post.comments) ? post.comments : [];
    const isLoggedIn = Boolean(activeSession() && activeSession().logged_in);

    const composerHtml = isLoggedIn
        ? `
            <form class="comment-form" data-post-id="${escapeHtml(post.id)}">
                <textarea
                    class="comment-input"
                    name="content"
                    maxlength="1000"
                    placeholder="Napište komentář k publikaci"
                    required
                ></textarea>
                <div class="comment-form-row">
                    <p class="comment-help">Komentář mohou přidávat jen přihlášení uživatelé.</p>
                    <button type="submit" class="btn btn-secondary comment-submit-btn">Odeslat komentář</button>
                </div>
                <p class="status-message comment-status" aria-live="polite"></p>
            </form>
        `
        : `
            <p class="comment-login-note">
                Komentovat mohou jen přihlášení uživatelé.
                <a href="${API_URL}/auth/login">Přihlásit se</a>
            </p>
        `;

    return `
        <section class="comments-panel">
            <div class="comments-heading">
                <strong>Komentáře</strong>
                <span class="comments-count">${formatCommentCount(comments.length)}</span>
            </div>
            <div class="comments-list">
                ${buildCommentsListHtml(post)}
            </div>
            ${composerHtml}
        </section>
    `;
}

function renderCommentsSection(card, post, postsStore = state.posts) {
    const commentsPanel = card.querySelector(".comments-panel");
    if (!commentsPanel) return;

    commentsPanel.outerHTML = buildCommentsSectionHtml(post);
    attachCommentSectionHandlers(card, post, postsStore);

    if (state.sort === "comments_desc") {
        renderFeedState();
    }
}

function attachCommentCreateHandler(card, post, postsStore) {
    const form = card.querySelector(".comment-form");
    if (!form) return;

    form.addEventListener("submit", async (event) => {
        event.preventDefault();

        const textarea = form.querySelector(".comment-input");
        const submitBtn = form.querySelector(".comment-submit-btn");
        const statusNode = form.querySelector(".comment-status");
        const content = textarea.value.trim();

        if (!content) {
            setStatusMessage(statusNode, "Komentář nesmí být prázdný.", "error");
            return;
        }

        if (content.length > 1000) {
            setStatusMessage(statusNode, "Komentář je příliš dlouhý.", "error");
            return;
        }

        try {
            setStatusMessage(statusNode, "Odesílám komentář...");
            textarea.disabled = true;
            submitBtn.disabled = true;

            const res = await apiPost(`/api/posts/${encodeURIComponent(post.id)}/comments`, { content });
            if (!res || !res.ok || !res.comment) {
                throw new Error("Nepodařilo se odeslat komentář.");
            }

            const targetPost = findPostById(post.id, postsStore) || post;
            targetPost.comments = Array.isArray(targetPost.comments) ? targetPost.comments : [];
            targetPost.comments.push(res.comment);
            renderCommentsSection(card, targetPost, postsStore);
        } catch (error) {
            console.error("Failed to create comment", error);
            textarea.disabled = false;
            submitBtn.disabled = false;
            setStatusMessage(statusNode, error.message || "Nepodařilo se odeslat komentář.", "error");
        }
    });
}

function attachCommentEditHandlers(card, post, postsStore) {
    card.querySelectorAll(".comment-edit-btn").forEach((button) => {
        button.addEventListener("click", () => {
            const commentItem = button.closest(".comment-item");
            const editForm = commentItem.querySelector(".comment-edit-form");
            const commentText = commentItem.querySelector(".comment-text");
            if (!editForm || !commentText) return;

            commentText.hidden = true;
            editForm.hidden = false;
            editForm.querySelector(".comment-edit-input")?.focus();
        });
    });

    card.querySelectorAll(".comment-cancel-btn").forEach((button) => {
        button.addEventListener("click", () => {
            const commentItem = button.closest(".comment-item");
            const editForm = commentItem.querySelector(".comment-edit-form");
            const commentText = commentItem.querySelector(".comment-text");
            if (!editForm || !commentText) return;

            editForm.hidden = true;
            commentText.hidden = false;
        });
    });

    card.querySelectorAll(".comment-edit-form").forEach((form) => {
        form.addEventListener("submit", async (event) => {
            event.preventDefault();

            const commentID = form.dataset.commentId;
            const input = form.querySelector(".comment-edit-input");
            const saveBtn = form.querySelector(".comment-save-btn");
            const cancelBtn = form.querySelector(".comment-cancel-btn");
            const statusNode = form.querySelector(".comment-status");
            const content = (input?.value || "").trim();

            if (!content) {
                setStatusMessage(statusNode, "Komentář nesmí být prázdný.", "error");
                return;
            }

            try {
                if (input) input.disabled = true;
                if (saveBtn) saveBtn.disabled = true;
                if (cancelBtn) cancelBtn.disabled = true;
                setStatusMessage(statusNode, "Ukládám komentář...");

                const res = await apiPut(`/api/posts/${encodeURIComponent(post.id)}/comments/${encodeURIComponent(commentID)}`, { content });
                if (!res || !res.ok || !res.comment) {
                    throw new Error("Nepodařilo se uložit komentář.");
                }

                const targetPost = findPostById(post.id, postsStore) || post;
                targetPost.comments = (targetPost.comments || []).map((comment) => (
                    comment.id === commentID ? res.comment : comment
                ));
                renderCommentsSection(card, targetPost, postsStore);
            } catch (error) {
                console.error("Failed to update comment", error);
                if (input) input.disabled = false;
                if (saveBtn) saveBtn.disabled = false;
                if (cancelBtn) cancelBtn.disabled = false;
                setStatusMessage(statusNode, error.message || "Komentář se nepodařilo uložit.", "error");
            }
        });
    });
}

function attachCommentDeleteHandlers(card, post, postsStore) {
    card.querySelectorAll(".comment-delete-btn").forEach((button) => {
        button.addEventListener("click", async () => {
            const commentID = button.dataset.commentId;
            if (!commentID) return;
            if (!window.confirm("Opravdu chcete komentář smazat?")) {
                return;
            }

            button.disabled = true;

            try {
                const res = await apiDelete(`/api/posts/${encodeURIComponent(post.id)}/comments/${encodeURIComponent(commentID)}`);
                if (!res || !res.ok) {
                    throw new Error("Komentář se nepodařilo smazat.");
                }

                const targetPost = findPostById(post.id, postsStore) || post;
                targetPost.comments = (targetPost.comments || []).filter((comment) => comment.id !== commentID);
                renderCommentsSection(card, targetPost, postsStore);
            } catch (error) {
                console.error("Failed to delete comment", error);
                button.disabled = false;
                window.alert(error.message || "Komentář se nepodařilo smazat.");
            }
        });
    });
}

function attachCommentModerationHandlers(card, post, postsStore) {
    card.querySelectorAll(".comment-hide-btn").forEach((button) => {
        button.addEventListener("click", async () => {
            const commentID = button.dataset.commentId;
            if (!commentID) return;
            if (!window.confirm("Opravdu chcete tento komentář skrýt z veřejné zdi?")) {
                return;
            }
            const note = window.prompt("Poznámka k moderaci (volitelné):", "");
            if (note === null) {
                return;
            }

            button.disabled = true;
            try {
                const res = await apiJsonRequest(`/api/moderation/comments/${encodeURIComponent(commentID)}/visibility`, {
                    method: "POST",
                    body: {
                        hidden: true,
                        reason_code: "manual_moderation",
                        note
                    }
                });
                if (!res || !res.ok) {
                    throw new Error("Komentář se nepodařilo skrýt.");
                }

                const targetPost = findPostById(post.id, postsStore) || post;
                targetPost.comments = (targetPost.comments || []).filter((comment) => comment.id !== commentID);
                renderCommentsSection(card, targetPost, postsStore);
            } catch (error) {
                console.error("Failed to hide comment", error);
                button.disabled = false;
                window.alert(error.message || "Komentář se nepodařilo skrýt.");
            }
        });
    });
}

function attachCommentSectionHandlers(card, post, postsStore = state.posts) {
    attachCommentCreateHandler(card, post, postsStore);
    attachCommentEditHandlers(card, post, postsStore);
    attachCommentDeleteHandlers(card, post, postsStore);
    attachCommentModerationHandlers(card, post, postsStore);
}

function buildInlineMapPopupHtml(capture, post) {
    const authorName = post.author_name || "Neznámý houbař";
    return window.HZDMapUI.buildPopupHtml({
        authorName,
        previewUrl: capture.public_url ? buildCaptureImageURL(capture, "popup") : "",
        altText: authorName,
        dateValue: capture.captured_at || post.created_at,
        actionHtml: `
            <button type="button" class="btn btn-secondary map-popup-action feed-map-open-btn" data-capture-id="${escapeHtml(capture.id)}">
                Otevřít ve fotkách
            </button>
        `
    });
}

function openPostCaptureLightbox(post, captureID) {
    const startIndex = post.captures.findIndex((capture) => capture.id === captureID);
    if (startIndex === -1 || !window.HZDLightbox) {
        return;
    }

    window.HZDLightbox.openCollection(post.captures, startIndex);
}

function attachInlineMapToggle(card, post, mapId, mapCaptures) {
    if (!mapCaptures.length || typeof L === "undefined") {
        return;
    }

    const toggleBtn = card.querySelector(".map-toggle-btn");
    const mapDiv = document.getElementById(mapId);
    let mapInitialized = false;

    if (!toggleBtn || !mapDiv) {
        return;
    }

    toggleBtn.addEventListener("click", () => {
        if (mapDiv.style.display === "block") {
            mapDiv.style.display = "none";
            toggleBtn.textContent = "Zobrazit na mapě";
            return;
        }

        mapDiv.style.display = "block";
        toggleBtn.textContent = "Skrýt mapu";

        if (!mapInitialized) {
            const postMap = L.map(mapId).setView([Number(mapCaptures[0].latitude), Number(mapCaptures[0].longitude)], 13);
            mapDiv._leaflet_map = postMap;
            L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
                attribution: "&copy; OpenStreetMap"
            }).addTo(postMap);

            const markers = mapCaptures.map((capture) => {
                const marker = L.marker([Number(capture.latitude), Number(capture.longitude)]);
                marker.bindPopup(buildInlineMapPopupHtml(capture, post));
                if (window.HZDMapUI) {
                    window.HZDMapUI.bindPopupAction(marker, ".feed-map-open-btn", () => {
                        openPostCaptureLightbox(post, capture.id);
                    });
                }
                return marker;
            });

            if (window.HZDMapClusters) {
                mapDiv._leaflet_layer = window.HZDMapClusters.replaceLayer(
                    postMap,
                    mapDiv._leaflet_layer,
                    markers,
                    {
                        clusterOptions: {
                            maxClusterRadius: 42,
                            spiderfyDistanceMultiplier: 1.12
                        }
                    }
                );
                window.HZDMapClusters.fitLayer(postMap, mapDiv._leaflet_layer, { padding: [10, 10], maxZoom: 15 });
            } else {
                const bounds = L.latLngBounds();
                markers.forEach((marker) => {
                    marker.addTo(postMap);
                    bounds.extend(marker.getLatLng());
                });
                if (bounds.isValid()) {
                    postMap.fitBounds(bounds, { padding: [10, 10], maxZoom: 15 });
                }
            }
            mapInitialized = true;
            return;
        }

        const existingMap = mapDiv._leaflet_map;
        if (existingMap) {
            existingMap.invalidateSize();
        }
    });
}

function attachPostManagementHandlers(card, post, postsStore, options) {
    const me = activeUser();
    const canManage = Boolean(options.allowPostManagement && me && Number(me.id) === Number(post.author_user_id));
    if (!canManage) {
        return;
    }

    const editBtn = card.querySelector(".post-edit-btn");
    const deleteBtn = card.querySelector(".post-delete-btn");

    if (editBtn) {
        editBtn.addEventListener("click", () => {
            window.location.href = `/edit-post.html?id=${encodeURIComponent(post.id)}`;
        });
    }

    if (deleteBtn) {
        deleteBtn.addEventListener("click", async () => {
            if (!window.confirm("Opravdu chcete tuto publikaci smazat?")) {
                return;
            }

            deleteBtn.disabled = true;
            try {
                const res = await apiDelete(`/api/posts/${encodeURIComponent(post.id)}`);
                if (!res || !res.ok) {
                    throw new Error("Publikaci se nepodařilo smazat.");
                }

                const nextPosts = postsStore.filter((item) => item.id !== post.id);
                postsStore.length = 0;
                nextPosts.forEach((item) => postsStore.push(item));
                card.remove();

                if (typeof options.onPostDeleted === "function") {
                    options.onPostDeleted(post, nextPosts);
                }
            } catch (error) {
                console.error("Failed to delete post", error);
                deleteBtn.disabled = false;
                window.alert(error.message || "Publikaci se nepodařilo smazat.");
            }
        });
    }
}

function attachPostModerationHandlers(card, post, postsStore, options) {
    if (!userCanModerateClient(activeUser())) {
        return;
    }

    const hideBtn = card.querySelector(".post-hide-btn");
    if (!hideBtn) {
        return;
    }

    hideBtn.addEventListener("click", async () => {
        if (!window.confirm("Opravdu chcete tuto publikaci skrýt z veřejné zdi?")) {
            return;
        }
        const note = window.prompt("Poznámka k moderaci (volitelné):", "");
        if (note === null) {
            return;
        }

        hideBtn.disabled = true;
        try {
            const res = await apiJsonRequest(`/api/moderation/posts/${encodeURIComponent(post.id)}/visibility`, {
                method: "POST",
                body: {
                    hidden: true,
                    reason_code: "manual_moderation",
                    note
                }
            });
            if (!res || !res.ok) {
                throw new Error("Publikaci se nepodařilo skrýt.");
            }

            const nextPosts = postsStore.filter((item) => item.id !== post.id);
            postsStore.length = 0;
            nextPosts.forEach((item) => postsStore.push(item));
            card.remove();

            if (typeof options.onPostDeleted === "function") {
                options.onPostDeleted(post, nextPosts);
            }
        } catch (error) {
            console.error("Failed to hide post", error);
            hideBtn.disabled = false;
            window.alert(error.message || "Publikaci se nepodařilo skrýt.");
        }
    });
}

function renderPosts(postsToRender, container, options = {}) {
    const postsStore = options.postsStore || state.posts;
    const isLoggedIn = Boolean(activeSession() && activeSession().logged_in);
    const me = activeUser();

    postsToRender.forEach((post) => {
        const card = document.createElement("article");
        card.className = "card feed-card";

        const avatarUrl = post.author_avatar || DEFAULT_AVATAR_URL;
        const authorName = post.author_name || "Neznámý houbař";

        let capturesHtml = "";
        const mapCaptures = [];

        if (post.captures && post.captures.length > 0) {
            capturesHtml = '<div class="feed-gallery">';
            post.captures.forEach((capture, idx) => {
                const url = escapeHtml(buildCaptureImageURL(capture, "thumb"));
                const accessBadge = buildCaptureAccessBadgeHtml(capture);
                capturesHtml += `
                    <div class="feed-photo-frame">
                        <img src="${url}" class="feed-photo" loading="lazy" data-idx="${idx}">
                        ${accessBadge}
                    </div>
                `;
                if (captureHasCoordinates(capture)) {
                    mapCaptures.push(capture);
                }
            });
            capturesHtml += "</div>";
        }

        const mapId = `feed-map-${post.id}`;
        const hasCoords = mapCaptures.length > 0;
        const mapBtnHtml = hasCoords
            ? `<button class="btn btn-secondary map-toggle-btn" data-target="${mapId}">Zobrazit na mapě</button>`
            : "";
        const mapDivHtml = hasCoords ? `<div id="${mapId}" class="feed-map-container"></div>` : "";
        const activeClass = post.is_liked_by_me ? "active" : "";
        const canManage = Boolean(options.allowPostManagement && me && Number(me.id) === Number(post.author_user_id));
        const canModerate = userCanModerateClient(me);
        const managementHtml = canManage
            ? `
                <div class="post-management-row">
                    <button type="button" class="btn btn-secondary post-edit-btn">Upravit publikaci</button>
                    <button type="button" class="btn btn-secondary post-delete-btn">Smazat publikaci</button>
                </div>
            `
            : "";
        const moderationHtml = canModerate
            ? `
                <div class="post-management-row">
                    <button type="button" class="btn btn-secondary post-hide-btn">Skrýt publikaci</button>
                </div>
            `
            : "";

        card.innerHTML = `
            <div class="feed-header">
                ${buildAuthorLinkHTML(post.author_user_id, avatarUrl, authorName, "feed-avatar", "feed-author")}
                <div class="feed-meta">
                    <span class="feed-date">${escapeHtml(formatDateTime(post.created_at))}</span>
                </div>
            </div>
            <div class="feed-content">
                ${escapeHtml(post.content).replace(/\n/g, "<br>")}
            </div>
            ${capturesHtml}
            ${mapDivHtml}
            ${managementHtml}
            ${moderationHtml}
            <div class="feed-actions">
                <button class="like-btn ${activeClass}" data-id="${post.id}">
                    <svg viewBox="0 0 24 24" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"></path>
                    </svg>
                    <span>${post.likes_count || 0}</span>
                </button>
                ${mapBtnHtml}
            </div>
            ${buildCommentsSectionHtml(post)}
        `;

        container.appendChild(card);

        const likeBtn = card.querySelector(".like-btn");
        if (likeBtn) {
            let likeRequestInFlight = false;
            likeBtn.addEventListener("click", async () => {
                if (likeRequestInFlight) return;
                if (!isLoggedIn) {
                    window.location.href = `${API_URL}/auth/login?next=feed`;
                    return;
                }

                const wasActive = likeBtn.classList.contains("active");
                const countNode = likeBtn.querySelector("span");
                const currentCount = parseInt(countNode.textContent, 10) || 0;

                likeRequestInFlight = true;
                likeBtn.disabled = true;
                likeBtn.classList.toggle("active");
                countNode.textContent = wasActive ? Math.max(0, currentCount - 1) : currentCount + 1;

                try {
                    const res = await apiPost(`/api/posts/${encodeURIComponent(post.id)}/like`);
                    if (!res || !res.ok) {
                        throw new Error("Nepodařilo se změnit lajk.");
                    }

                    countNode.textContent = res.likes_count;
                    likeBtn.classList.toggle("active", Boolean(res.is_liked));
                    post.likes_count = res.likes_count;
                    post.is_liked_by_me = res.is_liked;

                    if (state.sort === "likes_desc") {
                        renderFeedState();
                    }
                } catch (error) {
                    likeBtn.classList.toggle("active", wasActive);
                    countNode.textContent = currentCount;
                    console.error("Failed to toggle like", error);
                } finally {
                    likeRequestInFlight = false;
                    likeBtn.disabled = false;
                }
            });
        }

        attachInlineMapToggle(card, post, mapId, mapCaptures);

        card.querySelectorAll(".feed-photo").forEach((photo) => {
            photo.addEventListener("click", (event) => {
                if (!window.HZDLightbox) {
                    return;
                }
                window.HZDLightbox.openCollection(post.captures, parseInt(event.target.dataset.idx, 10));
            });
        });

        attachPostManagementHandlers(card, post, postsStore, options);
        attachPostModerationHandlers(card, post, postsStore, options);
        attachCommentSectionHandlers(card, post, postsStore);
    });
}

window.hzdFeedUI = {
    renderPosts
};

async function loadFeed(append = false) {
    const container = document.getElementById("feed-container");
    const loadMoreBtn = document.getElementById("load-more-feed-btn");

    if (!append) {
        container.innerHTML = '<p class="muted-copy feed-list-status">Načítám příspěvky...</p>';
    }

    try {
        const offset = (state.page - 1) * state.pageSize;
        const res = await apiGet(`/api/public/posts?limit=${state.pageSize}&offset=${offset}`);

        if (!res || !res.ok) {
            throw new Error("Nepodařilo se načíst příspěvky.");
        }

        const newPosts = res.posts || [];
        state.hasMore = newPosts.length >= state.pageSize;

        if (append) {
            state.posts = state.posts.concat(newPosts);
        } else {
            state.posts = newPosts;
            container.innerHTML = "";
        }

        if (state.posts.length === 0) {
            container.innerHTML = '<p class="muted-copy feed-list-status">Zatím nejsou žádné příspěvky k zobrazení.</p>';
            if (loadMoreBtn) loadMoreBtn.style.display = "none";
            return;
        }

        renderFeedState();

        if (loadMoreBtn) {
            loadMoreBtn.style.display = state.hasMore ? "inline-block" : "none";
        }
    } catch (error) {
        console.error("Failed to load feed", error);
        if (!append) {
            container.innerHTML = '<p class="muted-copy feed-list-status">Chyba při načítání zdi.</p>';
        }
    }
}

async function initFeedPage() {
    if (document.body.dataset.page !== "feed") return;

    state.session = await apiGet("/api/session");
    if (state.session && state.session.logged_in) {
        state.me = await apiGet("/api/me");
    }
    setAppIdentity(state.session, state.me);
    renderHeader(state.session, state.me);

    const sortSelect = document.getElementById("feed-sort-select");
    const sortForm = document.getElementById("feed-sort-form");
    if (sortSelect) {
        sortSelect.value = state.sort;
    }
    if (sortForm) {
        sortForm.addEventListener("submit", (event) => {
            event.preventDefault();
            applyFeedSort(sortSelect?.value || state.sort, { closeOnMobile: true });
        });
    }

    const loadMoreBtn = document.getElementById("load-more-feed-btn");
    if (loadMoreBtn) {
        loadMoreBtn.addEventListener("click", () => {
            if (state.hasMore) {
                state.page += 1;
                loadFeed(true);
            }
        });
    }

    state.page = 1;
    await loadFeed(false);
}

document.addEventListener("DOMContentLoaded", initFeedPage);
