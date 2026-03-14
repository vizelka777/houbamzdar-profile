const state = {
    posts: [],
    page: 1,
    pageSize: 10,
    hasMore: true,
    session: null,
    me: null
};

function findPostById(postID) {
    return state.posts.find((post) => post.id === postID) || null;
}

function formatCommentCount(count) {
    if (count === 1) return "1 komentář";
    if (count >= 2 && count <= 4) return `${count} komentáře`;
    return `${count} komentářů`;
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

        return `
            <article class="comment-item">
                <img src="${escapeHtml(avatarUrl)}" alt="Avatar komentujícího" class="comment-avatar">
                <div class="comment-body">
                    <div class="comment-meta">
                        <span class="comment-author">${escapeHtml(authorName)}</span>
                        <span class="comment-date">${escapeHtml(formatDateTime(comment.created_at))}</span>
                    </div>
                    <div class="comment-text">${content}</div>
                </div>
            </article>
        `;
    }).join("");
}

function buildCommentsSectionHtml(post) {
    const comments = Array.isArray(post.comments) ? post.comments : [];
    const isLoggedIn = Boolean(state.session && state.session.logged_in);

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

function renderCommentsSection(card, post) {
    const commentsPanel = card.querySelector(".comments-panel");
    if (!commentsPanel) return;

    commentsPanel.outerHTML = buildCommentsSectionHtml(post);
    attachCommentFormHandler(card, post);
}

function attachCommentFormHandler(card, post) {
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

            const res = await apiPost(`/api/posts/${encodeURIComponent(post.id)}/comments`, {
                content
            });

            if (!res || !res.ok || !res.comment) {
                throw new Error("Nepodařilo se odeslat komentář.");
            }

            const targetPost = findPostById(post.id) || post;
            targetPost.comments = Array.isArray(targetPost.comments) ? targetPost.comments : [];
            targetPost.comments.push(res.comment);

            renderCommentsSection(card, targetPost);
        } catch (error) {
            console.error("Failed to create comment", error);
            textarea.disabled = false;
            submitBtn.disabled = false;
            setStatusMessage(statusNode, error.message || "Nepodařilo se odeslat komentář.", "error");
        }
    });
}

async function loadFeed(append = false) {
    const container = document.getElementById("feed-container");
    const loadMoreBtn = document.getElementById("load-more-feed-btn");

    if (!append) {
        container.innerHTML = '<p class="muted-copy" style="text-align: center;">Načítám příspěvky...</p>';
    }

    try {
        const offset = (state.page - 1) * state.pageSize;
        const res = await apiGet(`/api/public/posts?limit=${state.pageSize}&offset=${offset}`);

        if (res && res.ok) {
            const newPosts = res.posts || [];
            state.hasMore = newPosts.length >= state.pageSize;

            if (append) {
                state.posts = state.posts.concat(newPosts);
            } else {
                state.posts = newPosts;
                container.innerHTML = "";
            }

            if (state.posts.length === 0) {
                container.innerHTML = '<p class="muted-copy" style="text-align: center;">Zatím nejsou žádné příspěvky k zobrazení.</p>';
                if (loadMoreBtn) loadMoreBtn.style.display = "none";
                return;
            }

            renderPosts(newPosts, container);

            if (loadMoreBtn) {
                loadMoreBtn.style.display = state.hasMore ? "inline-block" : "none";
            }
        }
    } catch (e) {
        console.error("Failed to load feed", e);
        if (!append) {
            container.innerHTML = '<p class="muted-copy" style="text-align: center;">Chyba při načítání zdi.</p>';
        }
    }
}

function renderPosts(postsToRender, container) {
    const isLoggedIn = Boolean(state.session && state.session.logged_in);

    postsToRender.forEach((post) => {
        const card = document.createElement("article");
        card.className = "card feed-card";

        const avatarUrl = post.author_avatar || "/default-avatar.png";
        const authorName = post.author_name || "Neznámý houbař";

        let capturesHtml = "";
        const captureUrls = [];
        let hasCoords = false;
        let hasFreeCoords = false;
        const mapData = [];

        if (post.captures && post.captures.length > 0) {
            capturesHtml = '<div class="feed-gallery">';
            post.captures.forEach((capture, idx) => {
                const url = capture.public_url
                    ? escapeHtml(capture.public_url)
                    : `${API_URL}/api/captures/${encodeURIComponent(capture.id)}/preview`;
                captureUrls.push(url);
                capturesHtml += `<img src="${url}" class="feed-photo" loading="lazy" data-idx="${idx}">`;
                if (capture.latitude && capture.longitude) {
                    hasCoords = true;
                    if (capture.coordinates_free) {
                        hasFreeCoords = true;
                    }
                    mapData.push({ lat: capture.latitude, lon: capture.longitude });
                }
            });
            capturesHtml += "</div>";
        }

        const mapId = `feed-map-${post.id}`;
        let mapBtnHtml = "";
        let mapDivHtml = "";
        if (hasCoords) {
            mapBtnHtml = `<button class="btn btn-secondary map-toggle-btn" data-target="${mapId}" style="margin-left:auto; font-size: 0.8rem; padding: 0.2rem 0.5rem;">Zobrazit na mapě</button>`;
            mapDivHtml = `<div id="${mapId}" class="feed-map-container"></div>`;
        }

        const activeClass = post.is_liked_by_me ? "active" : "";

        card.innerHTML = `
            <div class="feed-header">
                <img src="${escapeHtml(avatarUrl)}" alt="Avatar" class="feed-avatar">
                <div class="feed-meta">
                    <span class="feed-author">${escapeHtml(authorName)}</span>
                    <span class="feed-date">${escapeHtml(formatDateTime(post.created_at))}</span>
                </div>
            </div>
            <div class="feed-content">
                ${escapeHtml(post.content).replace(/\n/g, "<br>")}
            </div>
            ${capturesHtml}
            ${hasFreeCoords ? '<p class="coords-free-badge">✅ Souřadnice u fotek jsou zdarma</p>' : ""}
            ${mapDivHtml}
            <div class="feed-actions" style="display: flex; justify-content: flex-start; align-items: center; gap: 1rem;">
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

        // Обработчик лайка
        const likeBtn = card.querySelector(".like-btn");
        if (likeBtn) {
            let likeRequestInFlight = false;
            likeBtn.addEventListener("click", async () => {
                if (likeRequestInFlight) return;

                if (!isLoggedIn) {
                    window.location.href = `${API_URL}/auth/login?next=feed`;
                    return;
                }

                // Optimistic UI
                const wasActive = likeBtn.classList.contains("active");
                const countNode = likeBtn.querySelector("span");
                const currentCount = parseInt(countNode.textContent, 10) || 0;

                likeRequestInFlight = true;
                likeBtn.disabled = true;
                likeBtn.classList.toggle("active");
                countNode.textContent = wasActive ? Math.max(0, currentCount - 1) : currentCount + 1;

                try {
                    const res = await apiPost(`/api/posts/${encodeURIComponent(post.id)}/like`);
                    if (res && res.ok) {
                        countNode.textContent = res.likes_count;
                        if (res.is_liked) likeBtn.classList.add("active");
                        else likeBtn.classList.remove("active");
                        
                        // Update state
                        post.likes_count = res.likes_count;
                        post.is_liked_by_me = res.is_liked;
                    } else {
                        throw new Error();
                    }
                } catch (e) {
                    // Rollback
                    likeBtn.classList.toggle("active", wasActive);
                    countNode.textContent = currentCount;
                    console.error("Failed to toggle like", e);
                } finally {
                    likeRequestInFlight = false;
                    likeBtn.disabled = false;
                }
            });
        }

        if (hasCoords && typeof L !== "undefined") {
            const toggleBtn = card.querySelector(".map-toggle-btn");
            const mapDiv = document.getElementById(mapId);
            let mapInitialized = false;

            if (toggleBtn && mapDiv) {
                toggleBtn.addEventListener("click", () => {
                    if (mapDiv.style.display === "block") {
                        mapDiv.style.display = "none";
                        toggleBtn.textContent = "Zobrazit na mapě";
                    } else {
                        mapDiv.style.display = "block";
                        toggleBtn.textContent = "Skrýt mapu";
                        if (!mapInitialized) {
                            const postMap = L.map(mapId).setView([mapData[0].lat, mapData[0].lon], 13);
                            L.tileLayer("https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png", {
                                attribution: "&copy; OpenStreetMap"
                            }).addTo(postMap);

                            const bounds = L.latLngBounds();
                            mapData.forEach((point) => {
                                L.marker([point.lat, point.lon]).addTo(postMap);
                                bounds.extend([point.lat, point.lon]);
                            });

                            if (mapData.length > 1) {
                                postMap.fitBounds(bounds, { padding: [10, 10] });
                            }
                            mapInitialized = true;
                        }
                    }
                });
            }
        }

        const photos = card.querySelectorAll(".feed-photo");
        photos.forEach((photo) => {
            photo.addEventListener("click", (event) => {
                window.lightboxImages = captureUrls;
                window.lightboxMapData = post.captures.map((capture) =>
                    (capture.latitude && capture.longitude) ? { lat: capture.latitude, lon: capture.longitude } : null
                );
                window.currentLightboxIndex = parseInt(event.target.dataset.idx, 10);
                if (typeof openLightbox === "function") openLightbox();
            });
        });

        attachCommentFormHandler(card, post);
    });
}

async function initFeedPage() {
    if (document.body.dataset.page !== "feed") return;

    state.session = await apiGet("/api/session");
    if (state.session && state.session.logged_in) {
        state.me = await apiGet("/api/me");
    }
    renderHeader(state.session, state.me);

    const loadMoreBtn = document.getElementById("load-more-feed-btn");
    if (loadMoreBtn) {
        loadMoreBtn.addEventListener("click", () => {
            if (state.hasMore) {
                state.page++;
                loadFeed(true);
            }
        });
    }

    state.page = 1;
    await loadFeed(false);
}

document.addEventListener("DOMContentLoaded", initFeedPage);
