const state = {
    captures: [],
    selectedCaptureIds: new Set()
};

async function loadCapturesForSelection() {
    const grid = document.getElementById("post-captures-grid");
    if (!grid) return;

    try {
        // Fetch recent captures (first page, let's say 20 items to pick from)
        const result = await apiGet("/api/captures?page_size=20");
        if (result && result.ok) {
            state.captures = result.captures || [];
        }
    } catch (e) {
        console.error("Failed to load captures", e);
    }

    grid.innerHTML = "";

    if (state.captures.length === 0) {
        grid.innerHTML = '<p class="muted-copy">Žádné fotografie k dispozici.</p>';
        return;
    }

    state.captures.forEach(capture => {
        const item = document.createElement("div");
        item.className = "post-capture-item";
        item.dataset.id = capture.id;

        const imgUrl = `${API_URL}/api/captures/${encodeURIComponent(capture.id)}/preview`;

        item.innerHTML = `
            <img src="${escapeHtml(imgUrl)}" alt="Fotografie" loading="lazy">
            <div class="badge">✓</div>
        `;

        item.addEventListener("click", () => {
            if (state.selectedCaptureIds.has(capture.id)) {
                state.selectedCaptureIds.delete(capture.id);
                item.classList.remove("selected");
            } else {
                state.selectedCaptureIds.add(capture.id);
                item.classList.add("selected");
            }
        });

        grid.appendChild(item);
    });
}

async function handlePostSubmit(event) {
    event.preventDefault();
    const content = document.getElementById("post-content").value.trim();
    if (!content) return;

    const statusNode = document.getElementById("post-status");
    const submitBtn = document.getElementById("post-submit-btn");

    try {
        setStatusMessage(statusNode, "Publikuji...");
        submitBtn.disabled = true;

        const payload = {
            content: content,
            capture_ids: Array.from(state.selectedCaptureIds)
        };

        const res = await apiPost("/api/posts", payload);
        if (!res || !res.ok) {
            throw new Error("Nepodařilo se vytvořit publikaci.");
        }

        setStatusMessage(statusNode, "Úspěšně publikováno!", "success");
        setTimeout(() => {
            window.location.href = "/public-profile.html";
        }, 1500);
    } catch (err) {
        console.error(err);
        setStatusMessage(statusNode, err.message || "Vyskytla se chyba", "error");
        submitBtn.disabled = false;
    }
}

async function initCreatePostPageLogic() {
    if (document.body.dataset.page !== "create-post") return;

    // initCreatePostPage is called from app.js as well, but we can manage our local forms here
    const form = document.getElementById("create-post-form");
    if (form) {
        form.addEventListener("submit", handlePostSubmit);
    }

    await loadCapturesForSelection();
}

document.addEventListener("DOMContentLoaded", initCreatePostPageLogic);
