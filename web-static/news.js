const newsState = {
    latestCaptures: []
};

async function initNewsWidgets() {
    const session = await apiGet("/api/session");
    let me = null;
    if (session && session.logged_in) {
        me = await apiGet("/api/me");
    }

    setAppIdentity(session, me);
    renderHeader(session, me);
    initNewsSpeciesModal();

    await Promise.all([
        renderLatestPost(),
        renderLatestPhotos(),
        renderLatestUsers()
    ]);
}

function formatNewsGalleryRegionLabel(region) {
    const safeRegion = escapeHtml(region || "");
    return safeRegion.replace(/\s+([^\s]+)$/u, "<br>$1");
}

function buildNewsGallerySpeciesButton(capture) {
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

function closeNewsSpeciesModal() {
    const modal = document.getElementById("news-species-modal");
    if (!modal) {
        return;
    }
    modal.hidden = true;
    modal.setAttribute("aria-hidden", "true");
}

function openNewsSpeciesModal(captureID) {
    const modal = document.getElementById("news-species-modal");
    const body = document.getElementById("news-species-body");
    const meta = document.getElementById("news-species-meta");
    if (!modal || !body || !meta) {
        return;
    }

    const capture = newsState.latestCaptures.find((item) => item && item.id === captureID) || null;
    const entries = buildCaptureSpeciesEntries(capture);
    if (!capture || entries.length === 0) {
        return;
    }

    const authorName = String(capture.author_name || "Neznámý houbař").trim();
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

function initNewsSpeciesModal() {
    const modal = document.getElementById("news-species-modal");
    const closeButton = document.getElementById("news-species-close");
    if (!modal) {
        return;
    }

    modal.addEventListener("click", (event) => {
        if (event.target instanceof HTMLElement && event.target.hasAttribute("data-close-news-species-modal")) {
            closeNewsSpeciesModal();
        }
    });

    closeButton?.addEventListener("click", closeNewsSpeciesModal);
    window.addEventListener("keydown", (event) => {
        if (event.key === "Escape") {
            closeNewsSpeciesModal();
        }
    });
}

async function renderLatestPost() {
    const container = document.getElementById("home-latest-post");
    if (!container) return;

    try {
        container.className = "feed-list";
        const result = await apiGet("/api/public/posts?limit=1");
        if (!result || !result.ok || !result.posts || result.posts.length === 0) {
            container.innerHTML = "<p class='muted-copy'>Zatím žádné publikace.</p>";
            return;
        }

        container.innerHTML = "";
        const posts = result.posts.map((post) => {
            if (!post.author_user_id && post.user_id) {
                post.author_user_id = post.user_id;
            }
            return post;
        });

        renderPosts(posts, container, { postsStore: posts });
    } catch (error) {
        console.error("Failed to load latest post", error);
        container.innerHTML = "<p class='muted-copy'>Nepodařilo se načíst publikaci.</p>";
    }
}

async function renderLatestPhotos() {
    const container = document.getElementById("home-latest-photos");
    if (!container) return;

    try {
        container.className = "gallery-grid";
        const result = await apiGet("/api/public/captures?limit=6");
        if (!result || !result.ok || !result.captures || result.captures.length === 0) {
            container.innerHTML = "<p class='muted-copy gallery-grid-status'>Zatím žádné fotografie.</p>";
            return;
        }

        newsState.latestCaptures = result.captures.map((capture) => {
            if (!capture.author_user_id && capture.user_id) {
                capture.author_user_id = capture.user_id;
            }
            return capture;
        });

        container.innerHTML = newsState.latestCaptures.map((capture, idx) => {
            const avatarUrl = capture.author_avatar || DEFAULT_AVATAR_URL;
            const authorName = capture.author_name || "Neznámý houbař";
            const accessBadge = buildCaptureAccessBadgeHtml(capture);
            const authorURL = buildPublicProfileURL(capture.author_user_id);
            const region = buildCaptureKrajLabel(capture);
            const imageHtml = buildCaptureImageTag(capture, {
                variant: "thumb",
                alt: "Houbařský úlovek",
                loading: "lazy",
                sizes: "(max-width: 720px) 50vw, (max-width: 1200px) 33vw, 384px"
            });
            const speciesButton = buildNewsGallerySpeciesButton(capture);

            return `
                <div class="gallery-item" data-index="${idx}" tabindex="0" role="button" aria-label="Zobrazit detail fotky">
                    <div class="gallery-item-header">
                        <a href="${escapeHtml(authorURL)}" class="author-link">
                            <img src="${escapeHtml(avatarUrl)}" class="gallery-item-avatar" alt="Avatar">
                            <span class="gallery-item-author">${escapeHtml(authorName)}</span>
                        </a>
                    </div>
                    <div class="gallery-item-image">
                        ${imageHtml}
                        ${accessBadge}
                    </div>
                    <div class="gallery-item-copy">
                        ${region ? `
                            <div class="gallery-item-meta-row">
                                <p class="gallery-item-region">${formatNewsGalleryRegionLabel(region)}</p>
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
                window.HZDLightbox.openCollection(newsState.latestCaptures, Number(item.dataset.index || 0));
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
                openNewsSpeciesModal(button.dataset.captureId || "");
            });
        });
    } catch (error) {
        console.error("Failed to load latest photos", error);
        container.innerHTML = "<p class='muted-copy gallery-grid-status'>Nepodařilo se načíst fotografie.</p>";
    }
}

async function renderLatestUsers() {
    const container = document.getElementById("home-latest-users");
    if (!container) return;

    try {
        const result = await apiGet("/api/public/users?limit=8&sort=popularity");
        if (!result || !result.ok || !result.users || result.users.length === 0) {
            container.innerHTML = "<p class='muted-copy'>Zatím žádní houbaři.</p>";
            return;
        }

        const usersHtml = result.users.map((user) => {
            const avatarHtml = buildAvatarImageHtml(user.picture, user.preferred_username, "home-user-avatar");
            return `
                <a href="${buildPublicProfileURL(user.id)}" class="home-user-link">
                    ${avatarHtml}
                    <span class="home-user-name">${escapeHtml(user.preferred_username)}</span>
                </a>
            `;
        }).join("");

        container.innerHTML = `<div class="home-users-row">${usersHtml}</div>`;
    } catch (error) {
        console.error("Failed to load latest users", error);
        container.innerHTML = "<p class='muted-copy'>Nepodařilo se načíst houbaře.</p>";
    }
}

document.addEventListener("DOMContentLoaded", () => {
    if (document.body.dataset.page === "news") {
        initNewsWidgets();
    }
});
