const state = {
    posts: [],
    page: 1,
    pageSize: 10,
    hasMore: true
};

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
            if (newPosts.length < state.pageSize) {
                state.hasMore = false;
            } else {
                state.hasMore = true;
            }

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
    postsToRender.forEach(post => {
        const card = document.createElement("article");
        card.className = "card feed-card";

        const avatarUrl = post.author_avatar || "/default-avatar.png";
        const authorName = post.author_name || "Neznámý houbař";

        let capturesHtml = "";
        let captureUrls = [];
        let hasCoords = false;
        let mapData = [];
        
        if (post.captures && post.captures.length > 0) {
            capturesHtml = '<div class="feed-gallery">';
            post.captures.forEach((c, idx) => {
                const url = c.public_url ? escapeHtml(c.public_url) : `${API_URL}/api/captures/${encodeURIComponent(c.id)}/preview`;
                captureUrls.push(url);
                capturesHtml += `<img src="${url}" class="feed-photo" loading="lazy" data-idx="${idx}">`;
                if (c.latitude && c.longitude) {
                    hasCoords = true;
                    mapData.push({lat: c.latitude, lon: c.longitude});
                }
            });
            capturesHtml += '</div>';
        }

        const mapId = `feed-map-${post.id}`;
        let mapBtnHtml = '';
        let mapDivHtml = '';
        if (hasCoords) {
            mapBtnHtml = `<button class="btn btn-secondary map-toggle-btn" data-target="${mapId}" style="margin-left:auto; font-size: 0.8rem; padding: 0.2rem 0.5rem;">Zobrazit na mapě</button>`;
            mapDivHtml = `<div id="${mapId}" class="feed-map-container"></div>`;
        }

        card.innerHTML = `
            <div class="feed-header">
                <img src="${escapeHtml(avatarUrl)}" alt="Avatar" class="feed-avatar">
                <div class="feed-meta">
                    <span class="feed-author">${escapeHtml(authorName)}</span>
                    <span class="feed-date">${escapeHtml(formatDateTime(post.created_at))}</span>
                </div>
            </div>
            <div class="feed-content">
                ${escapeHtml(post.content).replace(/\n/g, '<br>')}
            </div>
            ${capturesHtml}
            ${mapDivHtml}
            <div class="feed-actions" style="display: flex; justify-content: flex-start; align-items: center; gap: 1rem;">
                <button class="like-btn" onclick="toggleLike(this, '${post.id}')">
                    <svg viewBox="0 0 24 24" stroke-linecap="round" stroke-linejoin="round">
                        <path d="M20.84 4.61a5.5 5.5 0 0 0-7.78 0L12 5.67l-1.06-1.06a5.5 5.5 0 0 0-7.78 7.78l1.06 1.06L12 21.23l7.78-7.78 1.06-1.06a5.5 5.5 0 0 0 0-7.78z"></path>
                    </svg>
                    <span>${post.likes_count || 0}</span>
                </button>
                ${mapBtnHtml}
            </div>
        `;

        container.appendChild(card);

        if (hasCoords && typeof L !== 'undefined') {
            const toggleBtn = card.querySelector('.map-toggle-btn');
            const mapDiv = document.getElementById(mapId);
            let mapInitialized = false;

            if (toggleBtn && mapDiv) {
                toggleBtn.addEventListener('click', () => {
                    if (mapDiv.style.display === 'block') {
                        mapDiv.style.display = 'none';
                        toggleBtn.textContent = 'Zobrazit na mapě';
                    } else {
                        mapDiv.style.display = 'block';
                        toggleBtn.textContent = 'Skrýt mapu';
                        if (!mapInitialized) {
                            const postMap = L.map(mapId).setView([mapData[0].lat, mapData[0].lon], 13);
                            L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
                                attribution: '&copy; OpenStreetMap'
                            }).addTo(postMap);
                            
                            const bounds = L.latLngBounds();
                            mapData.forEach(pt => {
                                L.marker([pt.lat, pt.lon]).addTo(postMap);
                                bounds.extend([pt.lat, pt.lon]);
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

        // Přidání posluchačů pro obrázky pro lightbox
        const photos = card.querySelectorAll('.feed-photo');
        photos.forEach(photo => {
            photo.addEventListener('click', (e) => {
                window.lightboxImages = captureUrls;
                window.lightboxMapData = post.captures.map(c => 
                    (c.latitude && c.longitude) ? {lat: c.latitude, lon: c.longitude} : null
                );
                window.currentLightboxIndex = parseInt(e.target.dataset.idx);
                if (typeof openLightbox === "function") openLightbox();
            });
        });
    });
}

function toggleLike(btn, postId) {
    // Stub
    const span = btn.querySelector('span');
    const svg = btn.querySelector('svg');
    let count = parseInt(span.textContent);
    
    if (svg.style.fill === 'currentColor') {
        svg.style.fill = 'none';
        svg.style.color = 'var(--text-muted)';
        count = Math.max(0, count - 1);
    } else {
        svg.style.fill = 'currentColor';
        svg.style.color = 'var(--primary-color)';
        count += 1;
    }
    span.textContent = count;
}

async function initFeedPage() {
    if (document.body.dataset.page !== "feed") return;

    const session = await apiGet("/api/session");
    const me = await apiGet("/api/me");
    
    if (session && me) {
        renderHeader(session, me);
    }

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