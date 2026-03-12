const state = {
    captures: [],
    selectedCaptureIds: new Set(),
    page: 1,
    totalPages: 1
};

async function loadCapturesForSelection(append = false) {
    const grid = document.getElementById("post-captures-grid");
    const loadMoreBtn = document.getElementById("load-more-captures-btn");
    if (!grid) return;

    if (!append) {
        grid.innerHTML = '<p class="muted-copy">Načítám fotografie...</p>';
    }

    try {
        const result = await apiGet(`/api/captures?page_size=10&page=${state.page}`);
        if (result && result.ok) {
            state.totalPages = result.total_pages || 1;
            const newCaptures = result.captures || [];
            
            if (append) {
                state.captures = state.captures.concat(newCaptures);
            } else {
                state.captures = newCaptures;
                grid.innerHTML = "";
            }

            if (state.captures.length === 0) {
                grid.innerHTML = '<p class="muted-copy">Žádné fotografie k dispozici.</p>';
                if (loadMoreBtn) loadMoreBtn.style.display = "none";
                return;
            }

            newCaptures.forEach(capture => {
                const item = document.createElement("div");
                item.className = "post-capture-item";
                if (state.selectedCaptureIds.has(capture.id)) {
                    item.classList.add("selected");
                }

                const imgUrl = `${API_URL}/api/captures/${encodeURIComponent(capture.id)}/preview`;

                item.innerHTML = `
                    <img src="${escapeHtml(imgUrl)}" alt="Fotografie" loading="lazy">
                    <div class="badge">✓</div>
                `;

                item.addEventListener("click", () => {
                    const statusNode = document.getElementById("post-status");
                    if (state.selectedCaptureIds.has(capture.id)) {
                        state.selectedCaptureIds.delete(capture.id);
                        item.classList.remove("selected");
                        setStatusMessage(statusNode, "");
                    } else {
                        if (state.selectedCaptureIds.size >= 9) {
                            setStatusMessage(statusNode, "Můžete vybrat maximálně 9 fotografií.", "error");
                            return;
                        }
                        state.selectedCaptureIds.add(capture.id);
                        item.classList.add("selected");
                        setStatusMessage(statusNode, "");
                    }
                });

                grid.appendChild(item);
            });

            if (loadMoreBtn) {
                loadMoreBtn.style.display = state.page < state.totalPages ? "inline-block" : "none";
            }
        }
    } catch (e) {
        console.error("Failed to load captures", e);
        if (!append) {
            grid.innerHTML = '<p class="muted-copy">Nepodařilo se načíst fotografie.</p>';
        }
    }
}

async function handlePostSubmit(event) {
    event.preventDefault();
    const content = document.getElementById("post-content").value.trim();
    if (!content) return;

    const statusNode = document.getElementById("post-status");
    const submitBtn = document.getElementById("post-submit-btn");

    if (state.selectedCaptureIds.size > 9) {
        setStatusMessage(statusNode, "Můžete vybrat maximálně 9 fotografií.", "error");
        return;
    }

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

    const form = document.getElementById("create-post-form");
    if (form) {
        form.addEventListener("submit", handlePostSubmit);
    }

    const loadMoreBtn = document.getElementById("load-more-captures-btn");
    if (loadMoreBtn) {
        loadMoreBtn.addEventListener("click", () => {
            if (state.page < state.totalPages) {
                state.page++;
                loadCapturesForSelection(true);
            }
        });
    }

    state.page = 1;
    await loadCapturesForSelection(false);
}

document.addEventListener("DOMContentLoaded", initCreatePostPageLogic);
