import { state as authState, fetchGallery, apiGet, API_URL } from './api.js';

let galleryState = {
    captures: [],
    page: 1,
    pageSize: 24,
    total: 0,
    totalPages: 0,
    filters: {
        species: "",
        kraj: "",
        okres: "",
        obec: "",
        sort: "published_desc"
    },
    isLoading: false
};

function escapeHtml(unsafe) {
    if (!unsafe) return "";
    return String(unsafe)
         .replace(/&/g, "&amp;")
         .replace(/</g, "&lt;")
         .replace(/>/g, "&gt;")
         .replace(/"/g, "&quot;")
         .replace(/'/g, "&#039;");
}

function buildCaptureImageURL(capture, size) {
    if (!capture) return "";
    if (capture.public_url) {
        let urlStr = capture.public_url;
        if (size === "thumb" && urlStr.includes("foto.houbamzdar.cz")) {
            urlStr += "?width=384&quality=68";
        }
        return urlStr;
    }
    return `${API_URL}/api/captures/${encodeURIComponent(capture.id)}/preview`;
}

function buildPublicProfileURL(userID) {
    if (!userID) return "#";
    return `/public-profile.html?user=${encodeURIComponent(userID)}`;
}

function buildCaptureKrajLabel(capture) {
    if (!capture.kraj_name) return "";
    let label = capture.kraj_name;
    if (capture.okres_name) label += `, ${capture.okres_name}`;
    if (capture.obec_name) label += ` (${capture.obec_name})`;
    return label;
}

function buildCaptureAccessBadgeHtml(capture) {
    if (capture.coordinates_free) {
        return `<span class="access-badge badge-free" title="Přesné souřadnice jsou veřejně dostupné" style="display:flex; align-items:center; justify-content:center; background:#4CAF50; color:white; width:22px; height:22px; border-radius:50%;">
                    <svg viewBox="0 0 24 24" width="12" height="12" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect><path d="M7 11V7a5 5 0 0 1 9.9-1"></path></svg>
                </span>`;
    }
    if (capture.coordinates_locked) {
        return `<span class="access-badge badge-locked" title="Přesné souřadnice si můžete odemknout" style="display:flex; align-items:center; justify-content:center; background:#d32f2f; color:white; width:22px; height:22px; border-radius:50%;">
                    <svg viewBox="0 0 24 24" width="12" height="12" stroke="currentColor" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="11" width="18" height="11" rx="2" ry="2"></rect><path d="M7 11V7a5 5 0 0 1 10 0v4"></path></svg>
                </span>`;
    }
    return "";
}

function buildGallerySpeciesButton(capture) {
    if (!capture.has_mushrooms || !capture.mushroom_species || capture.mushroom_species.length === 0) {
        return "";
    }
    const best = capture.mushroom_species[0];
    const percentage = Math.round(best.probability * 100);
    const label = best.czech_official_name || best.latin_name;
    let color = "#d32f2f"; // low
    if (best.probability >= 0.85) color = "#4CAF50"; // high
    else if (best.probability >= 0.60) color = "#f57c00"; // medium

    return `
        <button type="button" class="capture-species-btn" data-capture-id="${escapeHtml(capture.id)}" title="Rozpoznáno AI: ${escapeHtml(label)}" style="width:100%; display:flex; justify-content:space-between; align-items:center; background:#f4f7f4; border:1px solid #ccc; padding:6px 10px; border-radius:6px; cursor:pointer; font-size:0.8rem; margin-top:8px;">
            <span class="species-name" style="font-weight:600; text-align:left; overflow:hidden; text-overflow:ellipsis; white-space:nowrap;">${escapeHtml(label)}</span>
            <span class="species-prob" style="color:${color}; font-weight:bold; margin-left:10px;">${percentage}%</span>
        </button>
    `;
}

export function getGalleryHtml() {
    return `
        <div class="gallery-spa-wrapper">
            <h2 class="screen-title">Galerie úlovků</h2>
            
            <details id="gallery-overview" class="accordion card" style="background: white; border-radius: 12px; margin-bottom: 20px; box-shadow: 0 2px 10px rgba(0,0,0,0.05);">
                <summary class="accordion-summary" style="padding: 15px; font-weight: bold; cursor: pointer; color: var(--primary-color);">
                    <span class="accordion-title" style="margin:0;">Kritéria hledání (Filtry)</span>
                </summary>
                <div class="accordion-body" style="padding: 15px; border-top: 1px solid #eee;">
                    <form id="gallery-filter-form" class="public-capture-filter-form">
                        <div class="public-capture-filter-grid" style="display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 10px;">
                            <label class="public-capture-filter-field" style="display: flex; flex-direction: column; font-size: 0.9rem; color: #555;">
                                <span>Druh houby</span>
                                <input id="gallery-filter-species" class="filter-input" type="search" placeholder="např. hřib smrkový nebo boletus" style="margin-top: 5px; padding: 8px; border: 1px solid #ccc; border-radius: 6px;">
                            </label>
                            <label class="public-capture-filter-field" style="display: flex; flex-direction: column; font-size: 0.9rem; color: #555;">
                                <span>Kraj</span>
                                <input id="gallery-filter-kraj" class="filter-input" type="search" placeholder="např. Vysočina" style="margin-top: 5px; padding: 8px; border: 1px solid #ccc; border-radius: 6px;">
                            </label>
                            <label class="public-capture-filter-field" style="display: flex; flex-direction: column; font-size: 0.9rem; color: #555;">
                                <span>Okres</span>
                                <input id="gallery-filter-okres" class="filter-input" type="search" placeholder="jen pro sdílené souřadnice" style="margin-top: 5px; padding: 8px; border: 1px solid #ccc; border-radius: 6px;">
                            </label>
                            <label class="public-capture-filter-field" style="display: flex; flex-direction: column; font-size: 0.9rem; color: #555;">
                                <span>Obec</span>
                                <input id="gallery-filter-obec" class="filter-input" type="search" placeholder="jen pro sdílené souřadnice" style="margin-top: 5px; padding: 8px; border: 1px solid #ccc; border-radius: 6px;">
                            </label>
                            <label class="public-capture-filter-field" style="display: flex; flex-direction: column; font-size: 0.9rem; color: #555;">
                                <span>Řazení</span>
                                <select id="gallery-filter-sort" class="filter-input" style="margin-top: 5px; padding: 8px; border: 1px solid #ccc; border-radius: 6px;">
                                    <option value="published_desc">Nejnovější zveřejnění</option>
                                    <option value="captured_desc">Nejnovější nález</option>
                                    <option value="probability_desc">Nejvyšší pravděpodobnost</option>
                                    <option value="kraj_asc">Kraje A-Z</option>
                                    <option value="okres_asc">Okresy A-Z</option>
                                    <option value="obec_asc">Obce A-Z</option>
                                </select>
                            </label>
                        </div>
                        <div class="action-row" style="margin-top: 15px; display: flex; gap: 10px;">
                            <button type="submit" class="btn-primary" style="flex: 1; padding: 10px;">Použít filtry</button>
                            <button id="gallery-filter-reset" type="button" class="btn-primary" style="background-color: #999; padding: 10px;">Vymazat</button>
                        </div>
                    </form>
                    <p id="gallery-summary" class="muted-copy" style="margin-top: 15px; color: #888; font-size: 0.9rem; text-align: center;">Načítám fotografie...</p>
                </div>
            </details>

            <div id="gallery-container" class="gallery-grid" style="display: grid; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); gap: 15px;">
                <p class="muted-copy gallery-grid-status" style="grid-column: 1 / -1; text-align: center; color: #888; padding: 40px;">Načítám fotografie...</p>
            </div>

            <nav id="gallery-pagination" class="gallery-pagination" aria-label="Stránkování galerie" style="display: flex; justify-content: center; flex-wrap: wrap; gap: 5px; margin-top: 30px; margin-bottom: 20px;" hidden></nav>
        </div>

        <div id="gallery-species-modal" class="capture-species-modal" hidden aria-hidden="true" style="position: fixed; inset: 0; background: rgba(0,0,0,0.6); z-index: 1000; display: none; align-items: center; justify-content: center;">
            <div class="capture-species-modal-backdrop" style="position: absolute; inset: 0;"></div>
            <div class="capture-species-modal-dialog" role="dialog" aria-modal="true" style="background: white; border-radius: 12px; padding: 20px; width: 90%; max-width: 400px; position: relative; max-height: 80vh; overflow-y: auto;">
                <div class="capture-species-modal-head" style="display: flex; justify-content: space-between; align-items: center; border-bottom: 1px solid #eee; padding-bottom: 10px; margin-bottom: 15px;">
                    <div>
                        <p class="section-label" style="margin:0; font-size:0.8rem; color:#888;">Rozpoznané druhy</p>
                        <h2 id="gallery-species-title" style="margin:5px 0 0 0; font-size: 1.2rem; color: var(--primary-color);">Houby na fotografii</h2>
                    </div>
                    <button id="gallery-species-close" type="button" style="background:none; border:none; font-size:1.5rem; cursor:pointer; color:#555;">&times;</button>
                </div>
                <div id="gallery-species-body"></div>
            </div>
        </div>
    `;
}

function buildGalleryPaginationItems(current, total) {
    const delta = 2;
    const left = current - delta;
    const right = current + delta + 1;
    const items = [];

    for (let i = 1; i <= total; i++) {
        if (i === 1 || i === total || (i >= left && i < right)) {
            items.push(i);
        }
    }

    const withGaps = [];
    let previous = null;
    for (const item of items) {
        if (previous) {
            if (item - previous === 2) {
                withGaps.push(previous + 1);
            } else if (item - previous !== 1) {
                withGaps.push("gap");
            }
        }
        withGaps.push(item);
        previous = item;
    }
    return withGaps;
}

function renderGalleryPagination() {
    const pagination = document.getElementById("gallery-pagination");
    if (!pagination) return;

    if (galleryState.totalPages <= 1) {
        pagination.hidden = true;
        pagination.style.display = 'none';
        pagination.innerHTML = "";
        return;
    }

    const items = buildGalleryPaginationItems(galleryState.page, galleryState.totalPages);
    pagination.hidden = false;
    pagination.style.display = 'flex';
    
    let html = `<button type="button" class="btn-secondary page-btn" data-page="${galleryState.page - 1}" ${galleryState.page <= 1 ? "disabled" : ""} style="padding: 8px 12px; border-radius: 6px; border: 1px solid #ccc; background: #fff; cursor: pointer; min-width: 40px; text-align: center;">&laquo;</button>`;
    
    items.forEach((item) => {
        if (item === "gap") {
            html += `<span class="gallery-pagination-gap" aria-hidden="true" style="padding: 8px; color: #888;">…</span>`;
        } else {
            const active = item === galleryState.page;
            const bg = active ? 'var(--primary-color)' : '#fff';
            const col = active ? '#fff' : '#333';
            html += `<button type="button" class="page-btn" data-page="${item}" ${active ? "disabled" : ""} style="padding: 8px 12px; border-radius: 6px; border: 1px solid #ccc; background: ${bg}; color: ${col}; cursor: pointer; font-weight: ${active ? 'bold' : 'normal'}; min-width: 40px; text-align: center; ${active ? ' pointer-events: none;' : ''}">${item}</button>`;
        }
    });

    html += `<button type="button" class="btn-secondary page-btn" data-page="${galleryState.page + 1}" ${galleryState.page >= galleryState.totalPages ? "disabled" : ""} style="padding: 8px 12px; border-radius: 6px; border: 1px solid #ccc; background: #fff; cursor: pointer; min-width: 40px; text-align: center;">&raquo;</button>`;
    
    pagination.innerHTML = html;

    pagination.querySelectorAll('button[data-page]').forEach(btn => {
        btn.addEventListener('click', () => {
            const p = parseInt(btn.getAttribute('data-page'), 10);
            if (!isNaN(p) && p !== galleryState.page) {
                galleryState.page = p;
                loadGalleryData();
                window.scrollTo({ top: 0, behavior: 'smooth' });
            }
        });
    });
}

function updateGallerySummary() {
    const summary = document.getElementById("gallery-summary");
    if (!summary) return;

    if (galleryState.isLoading) {
        summary.textContent = "Načítám...";
        return;
    }

    if (galleryState.total === 0) {
        summary.textContent = "Žádné výsledky.";
        return;
    }

    const start = (galleryState.page - 1) * galleryState.pageSize + 1;
    const end = Math.min(galleryState.page * galleryState.pageSize, galleryState.total);
    summary.innerHTML = `Zobrazeno <strong>${start}–${end}</strong> z <strong>${galleryState.total}</strong> veřejných fotografií.`;
}

function renderGallery(container) {
    if (!container) return;

    if (galleryState.captures.length === 0) {
        container.innerHTML = '<p class="muted-copy gallery-grid-status" style="grid-column: 1 / -1; text-align: center; color: #888; padding: 40px;">Zatím nejsou sdíleny žádné fotografie pro tento filtr.</p>';
        return;
    }

    container.innerHTML = galleryState.captures.map((capture, idx) => {
        const url = escapeHtml(buildCaptureImageURL(capture, "thumb"));
        const avatarUrl = capture.author_avatar || "icons/logo.png";
        const authorName = capture.author_name || "Neznámý houbař";
        const accessBadge = buildCaptureAccessBadgeHtml(capture);
        const authorURL = buildPublicProfileURL(capture.author_user_id);
        const region = buildCaptureKrajLabel(capture);
        const speciesButton = buildGallerySpeciesButton(capture);

        return `
            <div class="gallery-item-card" data-index="${idx}" style="display: flex; flex-direction: column; border-radius: 12px; overflow: hidden; background: #fff; border: 1px solid #e0e5e0; box-shadow: 0 4px 12px rgba(0,0,0,0.05); transition: transform 0.2s;">
                <div class="gallery-item-header" style="padding: 10px; display: flex; align-items: center; gap: 8px; border-bottom: 1px solid #f0f0f0;">
                    <a href="${escapeHtml(authorURL)}" style="display: flex; align-items: center; gap: 8px; text-decoration: none; color: inherit; overflow: hidden; white-space: nowrap;">
                        <img src="${escapeHtml(avatarUrl)}" alt="Avatar" style="width: 28px; height: 28px; border-radius: 50%; object-fit: cover; background: #eee; flex-shrink: 0;">
                        <span style="font-weight: 600; font-size: 0.9rem; text-overflow: ellipsis; overflow: hidden;">${escapeHtml(authorName)}</span>
                    </a>
                </div>
                <div class="gallery-item-image" style="position: relative; aspect-ratio: 1; background: #e0e5e0; cursor: pointer;">
                    <img src="${url}" loading="lazy" alt="Houba" style="width: 100%; height: 100%; object-fit: cover;">
                    <div style="position: absolute; top: 10px; right: 10px;">
                        ${accessBadge}
                    </div>
                </div>
                <div class="gallery-item-copy" style="padding: 10px; font-size: 0.85rem; color: #555; display: flex; flex-direction: column; gap: 5px; flex-grow: 1;">
                    ${region ? `<div style="margin: 0; display:flex; gap:5px;"><span style="flex-shrink:0;">📍</span><span style="overflow:hidden; text-overflow:ellipsis;">${escapeHtml(region)}</span></div>` : ""}
                    ${speciesButton ? `<div style="margin-top: auto;">${speciesButton}</div>` : ""}
                </div>
            </div>
        `;
    }).join("");

    // Hook up modal buttons
    container.querySelectorAll('.capture-species-btn').forEach(btn => {
        btn.addEventListener('click', (e) => {
            e.stopPropagation();
            const cid = btn.getAttribute('data-capture-id');
            openSpeciesModal(cid);
        });
    });

    // Hook up image clicks for lightbox simulation
    container.querySelectorAll('.gallery-item-image').forEach((imgWrap, idx) => {
        imgWrap.addEventListener('click', () => {
            const capture = galleryState.captures[idx];
            const fullUrl = buildCaptureImageURL(capture, "original");
            window.open(fullUrl, '_blank'); // Simple lightbox fallback for SPA for now
        });
    });
}

function openSpeciesModal(captureID) {
    const capture = galleryState.captures.find(c => c.id === captureID);
    if (!capture || !capture.mushroom_species) return;

    const modal = document.getElementById('gallery-species-modal');
    const body = document.getElementById('gallery-species-body');
    
    let html = '<ul style="list-style:none; padding:0; margin:0;">';
    capture.mushroom_species.forEach(sp => {
        const pct = Math.round(sp.probability * 100);
        html += `
            <li style="display: flex; justify-content: space-between; padding: 12px 0; border-bottom: 1px solid #eee;">
                <span><strong>${escapeHtml(sp.czech_official_name || sp.latin_name)}</strong> <br><small style="color:#888;">${escapeHtml(sp.latin_name)}</small></span>
                <span style="font-weight:bold; color: var(--primary-color); font-size: 1.1rem;">${pct}%</span>
            </li>
        `;
    });
    html += '</ul>';
    
    body.innerHTML = html;
    modal.hidden = false;
    modal.style.display = 'flex';
}

function syncFiltersFromForm() {
    galleryState.filters.species = document.getElementById('gallery-filter-species').value.trim();
    galleryState.filters.kraj = document.getElementById('gallery-filter-kraj').value.trim();
    galleryState.filters.okres = document.getElementById('gallery-filter-okres').value.trim();
    galleryState.filters.obec = document.getElementById('gallery-filter-obec').value.trim();
    galleryState.filters.sort = document.getElementById('gallery-filter-sort').value;
}

function syncFormFromFilters() {
    document.getElementById('gallery-filter-species').value = galleryState.filters.species || "";
    document.getElementById('gallery-filter-kraj').value = galleryState.filters.kraj || "";
    document.getElementById('gallery-filter-okres').value = galleryState.filters.okres || "";
    document.getElementById('gallery-filter-obec').value = galleryState.filters.obec || "";
    document.getElementById('gallery-filter-sort').value = galleryState.filters.sort || "published_desc";
}

async function loadGalleryData() {
    const container = document.getElementById('gallery-container');
    galleryState.isLoading = true;
    updateGallerySummary();
    container.innerHTML = '<p class="muted-copy gallery-grid-status" style="grid-column: 1 / -1; text-align: center; color: #888; padding: 40px;">Načítám fotografie...</p>';
    
    const params = new URLSearchParams();
    params.set("page", String(galleryState.page));
    params.set("page_size", String(galleryState.pageSize));
    if (galleryState.filters.species) params.set("species", galleryState.filters.species);
    if (galleryState.filters.kraj) params.set("kraj", galleryState.filters.kraj);
    if (galleryState.filters.okres) params.set("okres", galleryState.filters.okres);
    if (galleryState.filters.obec) params.set("obec", galleryState.filters.obec);
    if (galleryState.filters.sort) params.set("sort", galleryState.filters.sort);

    const res = await apiGet(`/api/public/captures?${params.toString()}`);
    galleryState.isLoading = false;

    if (!res || !res.ok) {
        container.innerHTML = '<p class="muted-copy gallery-grid-status" style="grid-column: 1 / -1; text-align: center; color: red; padding: 40px;">Chyba při načítání.</p>';
        return;
    }

    galleryState.total = Number(res.total) || 0;
    galleryState.totalPages = Number(res.total_pages) || 0;
    galleryState.captures = res.captures || [];

    renderGallery(container);
    renderGalleryPagination();
    updateGallerySummary();
}

export function initGalleryScreen() {
    syncFormFromFilters();
    
    document.getElementById('gallery-filter-form').addEventListener('submit', (e) => {
        e.preventDefault();
        syncFiltersFromForm();
        galleryState.page = 1;
        loadGalleryData();
    });

    document.getElementById('gallery-filter-reset').addEventListener('click', () => {
        galleryState.filters = { species: "", kraj: "", okres: "", obec: "", sort: "published_desc" };
        syncFormFromFilters();
        galleryState.page = 1;
        loadGalleryData();
    });

    document.getElementById('gallery-species-close').addEventListener('click', () => {
        const modal = document.getElementById('gallery-species-modal');
        modal.hidden = true;
        modal.style.display = 'none';
    });
    
    document.querySelector('.capture-species-modal-backdrop').addEventListener('click', () => {
        const modal = document.getElementById('gallery-species-modal');
        modal.hidden = true;
        modal.style.display = 'none';
    });

    loadGalleryData();
}