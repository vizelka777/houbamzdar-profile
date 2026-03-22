const state = {
    captures: [],
    selectedCaptureIds: new Set(),
    page: 1,
    totalPages: 1,
    loadedPageStarts: [0],
    currentPageIndex: 0
};

function capturePickerButtons() {
    return {
        prev: document.getElementById("load-prev-captures-btn"),
        next: document.getElementById("load-more-captures-btn")
    };
}

function updateCapturePickerNavigation() {
    const { prev, next } = capturePickerButtons();
    const hasCaptures = state.captures.length > 0;
    const hasPreviousChunk = state.currentPageIndex > 0;
    const hasLoadedForwardChunk = state.currentPageIndex < state.loadedPageStarts.length - 1;
    const canLoadAnotherChunk = state.page < state.totalPages;
    const showNavigation = hasCaptures && (state.totalPages > 1 || state.loadedPageStarts.length > 1);

    if (prev) {
        prev.style.display = showNavigation ? "inline-flex" : "none";
        prev.disabled = !hasPreviousChunk;
    }

    if (next) {
        next.style.display = showNavigation ? "inline-flex" : "none";
        next.disabled = !(hasLoadedForwardChunk || canLoadAnotherChunk);
    }
}

function scrollCaptureChunkIntoView(chunkIndex, behavior = "smooth") {
    const grid = document.getElementById("post-captures-grid");
    const startIndex = state.loadedPageStarts[chunkIndex];
    if (!grid || !Number.isInteger(startIndex) || startIndex < 0) {
        return;
    }

    const target = grid.querySelector(`.post-capture-item[data-capture-index="${startIndex}"]`);
    if (!target) {
        return;
    }

    state.currentPageIndex = chunkIndex;
    updateCapturePickerNavigation();
    grid.scrollTo({
        top: Math.max(target.offsetTop - grid.offsetTop - 8, 0),
        behavior
    });
}

function validatePostContentInput(input, statusNode) {
    if (!input) return null;

    const content = input.value.trim();
    const maxLength = input.maxLength > 0 ? input.maxLength : 2000;

    if (!content) {
        setStatusMessage(statusNode, "Text publikace je povinný.", "error");
        return null;
    }

    if ([...content].length > maxLength) {
        setStatusMessage(statusNode, `Text publikace může mít maximálně ${maxLength} znaků.`, "error");
        return null;
    }

    return content;
}

async function loadCapturesForSelection(append = false) {
    const grid = document.getElementById("post-captures-grid");
    const { next } = capturePickerButtons();
    if (!grid) return;

    if (!append) {
        grid.innerHTML = '<p class="muted-copy">Načítám fotografie...</p>';
        state.loadedPageStarts = [0];
        state.currentPageIndex = 0;
        updateCapturePickerNavigation();
    }

    try {
        const result = await apiGet(`/api/captures?status=published&page_size=10&page=${state.page}`);
        if (result && result.ok) {
            state.totalPages = result.total_pages || 1;
            const newCaptures = result.captures || [];
            const appendStartIndex = append ? state.captures.length : 0;
            
            if (append) {
                state.captures = state.captures.concat(newCaptures);
                if (newCaptures.length > 0) {
                    state.loadedPageStarts.push(appendStartIndex);
                }
            } else {
                state.captures = newCaptures;
                grid.innerHTML = "";
            }

            if (state.captures.length === 0) {
                grid.innerHTML = '<p class="muted-copy">Zatím nemáte žádné publikované fotografie.</p>';
                updateCapturePickerNavigation();
                return;
            }

            newCaptures.forEach((capture, localIndex) => {
                const item = document.createElement("div");
                item.className = "post-capture-item";
                item.dataset.captureIndex = String(appendStartIndex + localIndex);
                if (state.selectedCaptureIds.has(capture.id)) {
                    item.classList.add("selected");
                }

                const imgUrl = `${API_URL}/api/captures/${encodeURIComponent(capture.id)}/preview`;
                const imageHtml = buildCaptureImageTag(capture, {
                    variant: "thumb",
                    alt: "Fotografie",
                    loading: "lazy",
                    sizes: "(max-width: 720px) 50vw, (max-width: 1200px) 33vw, 200px"
                }) || `<img src="${escapeHtml(imgUrl)}" alt="Fotografie" loading="lazy">`;

                item.innerHTML = `
                    ${imageHtml}
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

            if (append && newCaptures.length > 0) {
                state.currentPageIndex = state.loadedPageStarts.length - 1;
                updateCapturePickerNavigation();
                window.requestAnimationFrame(() => {
                    scrollCaptureChunkIntoView(state.currentPageIndex, "smooth");
                });
                return;
            }

            updateCapturePickerNavigation();
        }
    } catch (e) {
        console.error("Failed to load captures", e);
        if (!append) {
            grid.innerHTML = '<p class="muted-copy">Nepodařilo se načíst fotografie.</p>';
        }
        if (next) {
            next.disabled = false;
        }
        updateCapturePickerNavigation();
    }
}

async function handlePostSubmit(event) {
    event.preventDefault();
    const statusNode = document.getElementById("post-status");
    const contentInput = document.getElementById("post-content");
    const content = validatePostContentInput(contentInput, statusNode);
    if (!content) return;

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

    const { prev, next } = capturePickerButtons();
    if (prev) {
        prev.addEventListener("click", () => {
            if (state.currentPageIndex > 0) {
                scrollCaptureChunkIntoView(state.currentPageIndex - 1);
            }
        });
    }
    if (next) {
        next.addEventListener("click", async () => {
            if (state.currentPageIndex < state.loadedPageStarts.length - 1) {
                scrollCaptureChunkIntoView(state.currentPageIndex + 1);
                return;
            }
            if (state.page < state.totalPages) {
                state.page++;
                next.disabled = true;
                await loadCapturesForSelection(true);
            }
        });
    }

    state.page = 1;
    state.loadedPageStarts = [0];
    state.currentPageIndex = 0;
    await loadCapturesForSelection(false);
}

document.addEventListener("DOMContentLoaded", initCreatePostPageLogic);
