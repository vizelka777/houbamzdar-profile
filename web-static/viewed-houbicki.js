async function initViewedHoubickiPage() {
    const session = await apiGet("/api/session");
    if (!session || !session.logged_in) {
        window.location.href = "/";
        return;
    }

    const me = await apiGet("/api/me");
    if (!me) return;
    renderHeader(session, me);

    const statusNode = document.getElementById("unlocked-status");
    const listNode = document.getElementById("unlocked-list");
    if (!listNode) return;

    setStatusMessage(statusNode, "Načítám odemčené záznamy...");
    const res = await apiGet("/api/me/unlocked-captures?limit=100&offset=0");
    if (!res || !res.ok) {
        setStatusMessage(statusNode, "Nepodařilo se načíst odemčené záznamy.", "error");
        return;
    }

    const items = res.unlocked_captures || [];
    if (!items.length) {
        setStatusMessage(statusNode, "Zatím nemáte žádné odemčené houbičky.");
        return;
    }

    setStatusMessage(statusNode, "");
    listNode.innerHTML = "";

    items.forEach((item) => {
        const capture = item.capture || {};
        const hasCoords = capture.latitude && capture.longitude;
        const previewUrl = capture.public_url || `${API_URL}/api/captures/${encodeURIComponent(capture.id)}/preview`;
        const coordsLabel = hasCoords
            ? `${Number(capture.latitude).toFixed(6)}, ${Number(capture.longitude).toFixed(6)}`
            : "Souřadnice nejsou dostupné";
        const postLink = item.post_id
            ? `/feed.html#post-${encodeURIComponent(item.post_id)}`
            : null;

        const card = document.createElement("article");
        card.className = "capture-item";
        card.innerHTML = `
            <img src="${escapeHtml(previewUrl)}" alt="${escapeHtml(capture.original_file_name || "Nález")}" class="capture-thumb" loading="lazy">
            <div class="capture-meta">
                <h3>${escapeHtml(capture.original_file_name || "Nález")}</h3>
                <p><strong>Souřadnice:</strong> ${escapeHtml(coordsLabel)}</p>
                <p><strong>Odemčeno:</strong> ${escapeHtml(formatDateTime(item.unlocked_at))}</p>
                <p><strong>Autor:</strong> ${escapeHtml(item.author_name || "Neznámý autor")}</p>
                <p><strong>Cena:</strong> ${escapeHtml(String(item.cost || 0))}</p>
                <p>
                    ${postLink ? `<a href="${postLink}">Otevřít původní post</a>` : "Bez odkazu na post"}
                    · <a href="/public-profile.html">Profil autora</a>
                </p>
            </div>
        `;
        listNode.appendChild(card);
    });
}

window.addEventListener("DOMContentLoaded", () => {
    if (document.body.dataset.page === "viewed-houbicki") {
        initViewedHoubickiPage();
    }
});
