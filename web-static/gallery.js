const state = {
    captures: [],
    page: 1,
    pageSize: 24,
    hasMore: true,
    session: null,
    me: null
};

async function loadGallery(append = false) {
    const container = document.getElementById("gallery-container");
    const loadMoreBtn = document.getElementById("load-more-gallery-btn");

    if (!append) {
        container.innerHTML = '<p class="muted-copy" style="grid-column: 1 / -1; text-align: center;">Načítám fotografie...</p>';
    }

    try {
        const offset = (state.page - 1) * state.pageSize;
        const res = await apiGet(`/api/public/captures?limit=${state.pageSize}&offset=${offset}`);
        
        if (res && res.ok) {
            const newCaptures = res.captures || [];
            if (newCaptures.length < state.pageSize) {
                state.hasMore = false;
            } else {
                state.hasMore = true;
            }

            if (append) {
                state.captures = state.captures.concat(newCaptures);
            } else {
                state.captures = newCaptures;
                container.innerHTML = "";
            }

            if (state.captures.length === 0) {
                container.innerHTML = '<p class="muted-copy" style="grid-column: 1 / -1; text-align: center;">Zatím nejsou sdíleny žádné fotografie.</p>';
                if (loadMoreBtn) loadMoreBtn.style.display = "none";
                return;
            }

            renderGallery(newCaptures, container);

            if (loadMoreBtn) {
                loadMoreBtn.style.display = state.hasMore ? "inline-block" : "none";
            }
        }
    } catch (e) {
        console.error("Failed to load gallery", e);
        if (!append) {
            container.innerHTML = '<p class="muted-copy" style="grid-column: 1 / -1; text-align: center;">Chyba při načítání galerie.</p>';
        }
    }
}

function renderGallery(capturesToRender, container) {
    window.lightboxImages = state.captures.map((capture) => buildCaptureImageURL(capture));
    window.lightboxCaptureData = state.captures;
    window.lightboxMapData = state.captures.map((capture) => buildCaptureMapData(capture));

    capturesToRender.forEach((capture, idx) => {
        const item = document.createElement("div");
        item.className = "gallery-item";

        const url = escapeHtml(buildCaptureImageURL(capture));
        const avatarUrl = capture.author_avatar || "/default-avatar.png";
        const authorName = capture.author_name || "Neznámý houbař";
        const accessBadge = buildCaptureAccessBadgeHtml(capture);
        const globalIdx = (state.page - 1) * state.pageSize + idx;
        const authorURL = buildPublicProfileURL(capture.author_user_id);

        item.innerHTML = `
            <div class="gallery-item-header">
                <a href="${escapeHtml(authorURL)}" class="author-link">
                    <img src="${escapeHtml(avatarUrl)}" class="gallery-item-avatar" alt="Avatar">
                    <span class="gallery-item-author">${escapeHtml(authorName)}</span>
                </a>
            </div>
            <div class="gallery-item-image">
                <img src="${url}" loading="lazy" alt="Houbařský úlovek">
                ${accessBadge}
            </div>
        `;

        item.addEventListener('click', () => {
            window.currentLightboxIndex = globalIdx;
            if (typeof openLightbox === "function") openLightbox();
        });

        item.querySelector(".author-link")?.addEventListener("click", (event) => {
            event.stopPropagation();
        });

        container.appendChild(item);
    });
}

async function initGalleryPage() {
    if (document.body.dataset.page !== "gallery") return;

    state.session = await apiGet("/api/session");
    if (state.session && state.session.logged_in) {
        state.me = await apiGet("/api/me");
    }

    setAppIdentity(state.session, state.me);
    renderHeader(state.session, state.me);

    const loadMoreBtn = document.getElementById("load-more-gallery-btn");
    if (loadMoreBtn) {
        loadMoreBtn.addEventListener("click", () => {
            if (state.hasMore) {
                state.page++;
                loadGallery(true);
            }
        });
    }

    state.page = 1;
    await loadGallery(false);
}

document.addEventListener("DOMContentLoaded", initGalleryPage);
