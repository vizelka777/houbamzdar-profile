const state = {
    captures: [],
    page: 1,
    pageSize: 24,
    hasMore: true
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
    // Aktualizace polí pro lightbox
    window.lightboxImages = state.captures.map(c => 
        c.public_url ? escapeHtml(c.public_url) : `${API_URL}/api/captures/${encodeURIComponent(c.id)}/preview`
    );
    window.lightboxMapData = state.captures.map(c => 
        (c.latitude && c.longitude && !c.coordinates_locked) ? { lat: c.latitude, lon: c.longitude } : null
    );
    window.lightboxCaptureIds = state.captures.map(c => c.id || null);
    window.lightboxCoordinatesLocked = state.captures.map(c => Boolean(c.coordinates_locked));

    capturesToRender.forEach((capture, idx) => {
        const item = document.createElement("div");
        item.className = "gallery-item";

        const url = capture.public_url ? escapeHtml(capture.public_url) : `${API_URL}/api/captures/${encodeURIComponent(c.id)}/preview`;
        const avatarUrl = capture.author_avatar || "/default-avatar.png";
        const authorName = capture.author_name || "Neznámý houbař";
        
        // Výpočet globálního indexu pro lightbox
        const globalIdx = (state.page - 1) * state.pageSize + idx;

        item.innerHTML = `
            <div class="gallery-item-header">
                <img src="${escapeHtml(avatarUrl)}" class="gallery-item-avatar" alt="Avatar">
                <span class="gallery-item-author">${escapeHtml(authorName)}</span>
            </div>
            <div class="gallery-item-image">
                <img src="${url}" loading="lazy" alt="Houbařský úlovek">
                ${capture.latitude && capture.longitude && !capture.coordinates_locked ? '<svg viewBox="0 0 24 24" style="position: absolute; bottom: 8px; right: 8px; width: 20px; height: 20px; fill: white; drop-shadow(0 2px 2px rgba(0,0,0,0.5));"><path d="M12 2C8.13 2 5 5.13 5 9c0 5.25 7 13 7 13s7-7.75 7-13c0-3.87-3.13-7-7-7zm0 9.5c-1.38 0-2.5-1.12-2.5-2.5s1.12-2.5 2.5-2.5 2.5 1.12 2.5 2.5-1.12 2.5-2.5 2.5z"/></svg>' : ''}
                ${capture.coordinates_locked ? '<button type="button" class="btn btn-secondary unlock-coordinates-btn" style="position:absolute; left:8px; bottom:8px; font-size:0.75rem; padding:0.2rem 0.4rem;">Открыть координаты за 1 houbičku</button>' : ''}
            </div>
        `;

        const unlockBtn = item.querySelector('.unlock-coordinates-btn');
        if (unlockBtn) {
            unlockBtn.addEventListener('click', async (event) => {
                event.stopPropagation();
                unlockBtn.disabled = true;
                unlockBtn.textContent = 'Otevírám...';
                try {
                    const res = await apiPost(`/api/captures/${encodeURIComponent(capture.id)}/unlock-coordinates`);
                    if (!res || !res.ok) throw new Error('Unlock failed');
                    await loadGallery(false);
                } catch (error) {
                    console.error('Failed to unlock coordinates', error);
                    unlockBtn.disabled = false;
                    unlockBtn.textContent = 'Открыть координаты за 1 houbičku';
                }
            });
        }

        item.addEventListener('click', () => {
            window.lightboxCaptureIds = state.captures.map(c => c.id || null);
            window.lightboxCoordinatesLocked = state.captures.map(c => Boolean(c.coordinates_locked));
            window.currentLightboxIndex = globalIdx;
            if (typeof openLightbox === "function") openLightbox();
        });

        container.appendChild(item);
    });
}

async function initGalleryPage() {
    if (document.body.dataset.page !== "gallery") return;

    const session = await apiGet("/api/session");
    const me = await apiGet("/api/me");
    
    if (session && me) {
        renderHeader(session, me);
    } else {
        renderHeader(session, null);
    }

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