const state = {
    captures: [],
    selectedCaptureIds: new Set(),
    page: 1,
    totalPages: 1,
    postId: null
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

async function handleEditPostSubmit(event) {
    event.preventDefault();
    if (!state.postId) return;

    const content = document.getElementById("post-content").value.trim();
    if (!content) return;

    const statusNode = document.getElementById("post-status");
    const submitBtn = document.getElementById("post-submit-btn");

    if (state.selectedCaptureIds.size > 9) {
        setStatusMessage(statusNode, "Můžete vybrat maximálně 9 fotografií.", "error");
        return;
    }

    try {
        setStatusMessage(statusNode, "Ukládám změny...");
        submitBtn.disabled = true;

        const payload = {
            content: content,
            capture_ids: Array.from(state.selectedCaptureIds)
        };

        const response = await fetch(`${API_URL}/api/posts/${encodeURIComponent(state.postId)}`, {
            method: "PUT",
            credentials: "include",
            headers: {
                "Content-Type": "application/json"
            },
            body: JSON.stringify(payload)
        });

        if (!response.ok) {
            throw new Error("Nepodařilo se uložit změny.");
        }

        setStatusMessage(statusNode, "Změny úspěšně uloženy!", "success");
        setTimeout(() => {
            window.location.href = "/public-profile.html";
        }, 1500);
    } catch (err) {
        console.error(err);
        setStatusMessage(statusNode, err.message || "Vyskytla se chyba", "error");
        submitBtn.disabled = false;
    }
}

async function initEditPostPageLogic() {
    if (document.body.dataset.page !== "edit-post") return;

    const urlParams = new URLSearchParams(window.location.search);
    state.postId = urlParams.get("id");

    if (!state.postId) {
        window.location.href = "/public-profile.html";
        return;
    }

    try {
        const res = await apiGet(`/api/posts/${encodeURIComponent(state.postId)}`);
        if (res && res.ok && res.post) {
            document.getElementById("post-content").value = res.post.content || "";
            if (res.post.captures) {
                res.post.captures.forEach(c => state.selectedCaptureIds.add(c.id));
            }
        } else {
            throw new Error("Post not found");
        }
    } catch (e) {
        console.error(e);
        alert("Příspěvek nebyl nalezen.");
        window.location.href = "/public-profile.html";
        return;
    }

    const form = document.getElementById("edit-post-form");
    if (form) {
        form.addEventListener("submit", handleEditPostSubmit);
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

document.addEventListener("DOMContentLoaded", initEditPostPageLogic);
