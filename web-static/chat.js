const chatState = {
    session: null,
    me: null,
    token: "",
    tokenExpiresAt: 0,
    chatApiBaseUrl: CHAT_API_URL,
    publicRooms: [],
    directRooms: [],
    activeRoom: null,
    messages: [],
    directRoomsLimit: 20,
    directRoomsOffset: 0,
    directRoomsHasMore: false,
    directRoomsLoading: false,
    directLoadMoreObserver: null,
    directLoadMoreScrollHandler: null,
    directLoadMoreScrollFrame: 0,
    pollingHandle: null,
    pollingBusy: false
};

const PUBLIC_CHAT_VIEW = "public";
const MESSAGES_VIEW = "messages";
const DIRECT_FOCUS_VIEW = "direct-focus";
const PUBLIC_CHAT_HISTORY_LIMIT = 200;
const DIRECT_CHAT_HISTORY_LIMIT = 80;

function activeChatRoomId() {
    return String(chatState.activeRoom?.id || "").trim();
}

function pendingDirectUserID() {
    const params = new URLSearchParams(window.location.search || "");
    const value = Number(params.get("dm") || 0);
    if (!Number.isFinite(value) || value <= 0) {
        return 0;
    }
    return value;
}

function clearPendingDirectUserID() {
    const url = new URL(window.location.href);
    if (!url.searchParams.has("dm")) {
        return;
    }
    url.searchParams.delete("dm");
    const nextSearch = url.searchParams.toString();
    const nextURL = `${url.pathname}${nextSearch ? `?${nextSearch}` : ""}${url.hash}`;
    window.history.replaceState({}, "", nextURL);
}

function defaultChatPageMode() {
    return document.body?.dataset.chatDefaultView === MESSAGES_VIEW ? MESSAGES_VIEW : PUBLIC_CHAT_VIEW;
}

function chatPageMode() {
    const params = new URLSearchParams(window.location.search || "");
    if (pendingDirectUserID() > 0) {
        return DIRECT_FOCUS_VIEW;
    }
    if (params.get("view") === MESSAGES_VIEW) {
        return MESSAGES_VIEW;
    }
    return defaultChatPageMode();
}

function isMessagesView() {
    const mode = chatPageMode();
    return mode === MESSAGES_VIEW || mode === DIRECT_FOCUS_VIEW;
}

function isPublicChatView() {
    return chatPageMode() === PUBLIC_CHAT_VIEW;
}

function isMessagesListView() {
    return chatPageMode() === MESSAGES_VIEW;
}

function isFocusedDirectView() {
    return chatPageMode() === DIRECT_FOCUS_VIEW;
}

function updateChatViewURL(view, { replace = false } = {}) {
    const url = new URL(window.location.href);
    if (view === MESSAGES_VIEW) {
        url.searchParams.set("view", MESSAGES_VIEW);
    } else {
        url.searchParams.delete("view");
        url.searchParams.delete("dm");
    }
    const nextSearch = url.searchParams.toString();
    const nextURL = `${url.pathname}${nextSearch ? `?${nextSearch}` : ""}${url.hash}`;
    window.history[replace ? "replaceState" : "pushState"]({}, "", nextURL);
    document.documentElement.setAttribute("data-chat-page-mode", view === MESSAGES_VIEW ? MESSAGES_VIEW : PUBLIC_CHAT_VIEW);
}

function isChatModerator() {
    return Boolean(chatState.me && (chatState.me.is_moderator || chatState.me.is_admin));
}

function hasMeaningfulChatDate(dateString) {
    const raw = String(dateString || "").trim();
    if (!raw || raw.startsWith("0001-01-01")) {
        return false;
    }
    const parsed = Date.parse(raw);
    return !Number.isNaN(parsed);
}

function isDeletedMessage(message) {
    return hasMeaningfulChatDate(message?.deleted_at);
}

function deletedMessageCopy() {
    return "Zpráva byla smazána.";
}

function canDeleteMessage(message) {
    if (!chatState.me || !message || isDeletedMessage(message)) {
        return false;
    }
    if (isChatModerator() && chatState.activeRoom?.kind === "public") {
        return true;
    }
    return Number(message.author_user_id) === Number(chatState.me.id);
}

function findKnownRoom(roomID) {
    return [...chatState.publicRooms, ...chatState.directRooms].find((room) => String(room.id) === String(roomID)) || null;
}

function roomLastActivityValue(room) {
    return Date.parse(room?.last_message?.created_at || room?.created_at || "") || 0;
}

function sortedDirectRooms() {
    return [...chatState.directRooms].sort((left, right) => roomLastActivityValue(right) - roomLastActivityValue(left));
}

function roomDisplayTitle(room) {
    if (!room) {
        return "Chat";
    }
    if (room.kind === "dm") {
        return room.other_user_name || room.title || "Soukromá konverzace";
    }
    return room.title || room.slug || "Místnost";
}

function roomDisplayMeta(room) {
    if (!room?.last_message) {
        return room.kind === "dm" ? "Zatím bez zpráv" : "Veřejná místnost";
    }
    if (isDeletedMessage(room.last_message)) {
        return deletedMessageCopy();
    }
    const author = room.last_message.author_name || "houbař";
    const excerpt = String(room.last_message.content || "").trim();
    return `${author}: ${excerpt.slice(0, 56)}${excerpt.length > 56 ? "…" : ""}`;
}

function roomPreviewSnippet(room, maxChars = 20) {
    if (!room?.last_message) {
        return "Zatím bez zpráv";
    }
    if (isDeletedMessage(room.last_message)) {
        return deletedMessageCopy();
    }
    const raw = String(room.last_message.content || "");
    if (!raw) {
        return "Zatím bez zpráv";
    }
    return raw.length > maxChars ? `${raw.slice(0, maxChars)}…` : raw;
}

function directRoomAvatar(room) {
    return room?.other_user_avatar || DEFAULT_AVATAR_URL;
}

function escapeMessageContent(content) {
    return escapeHtml(content || "").replace(/\n/g, "<br>");
}

async function ensureChatToken(forceRefresh = false) {
    const now = Date.now();
    if (!forceRefresh && chatState.token && chatState.tokenExpiresAt - now > 30000) {
        return chatState.token;
    }

    const response = await fetch(`${API_URL}/api/chat/token`, {
        method: "POST",
        credentials: "include"
    });
    const { payload, message } = await parseApiResponse(response);
    if (!response.ok || !payload?.token) {
        throw new Error(message || "Nepodařilo se získat chat token.");
    }

    chatState.token = payload.token;
    chatState.tokenExpiresAt = Date.parse(payload.expires_at || "") || now + 240000;
    if (payload.api_base_url) {
        chatState.chatApiBaseUrl = payload.api_base_url;
    }
    return chatState.token;
}

async function chatJsonRequest(path, options = {}) {
    let refreshed = false;

    while (true) {
        const token = await ensureChatToken(refreshed);
        const requestOptions = {
            method: options.method || "GET",
            headers: {
                Authorization: `Bearer ${token}`
            }
        };

        if (options.body !== undefined) {
            requestOptions.headers["Content-Type"] = "application/json";
            requestOptions.body = JSON.stringify(options.body);
        }

        const response = await fetch(`${chatState.chatApiBaseUrl}${path}`, requestOptions);
        const { payload, message } = await parseApiResponse(response);
        if (response.status === 401 && !refreshed) {
            chatState.token = "";
            chatState.tokenExpiresAt = 0;
            refreshed = true;
            continue;
        }
        if (!response.ok) {
            throw new Error(message || `Chat request failed (${response.status})`);
        }
        return payload;
    }
}

async function parseApiResponse(response) {
    const raw = await response.text();
    if (!raw) {
        return {
            payload: null,
            message: ""
        };
    }

    try {
        const payload = JSON.parse(raw);
        return {
            payload,
            message: payload?.message || payload?.error || ""
        };
    } catch (_error) {
        return {
            payload: null,
            message: String(raw || "").trim()
        };
    }
}

function disconnectDirectLoadMoreObserver() {
    if (chatState.directLoadMoreObserver) {
        chatState.directLoadMoreObserver.disconnect();
        chatState.directLoadMoreObserver = null;
    }
}

function disconnectDirectLoadMoreScrollHandler() {
    if (chatState.directLoadMoreScrollFrame) {
        window.cancelAnimationFrame(chatState.directLoadMoreScrollFrame);
        chatState.directLoadMoreScrollFrame = 0;
    }
    if (chatState.directLoadMoreScrollHandler) {
        window.removeEventListener("scroll", chatState.directLoadMoreScrollHandler);
        window.removeEventListener("resize", chatState.directLoadMoreScrollHandler);
        chatState.directLoadMoreScrollHandler = null;
    }
}

function syncDirectLoadMoreTrigger() {
    const loadMoreButton = document.getElementById("chat-direct-load-more");
    if (!loadMoreButton) {
        disconnectDirectLoadMoreObserver();
        disconnectDirectLoadMoreScrollHandler();
        return;
    }

    loadMoreButton.textContent = chatState.directRoomsLoading ? "Načítám další…" : "Načíst další";
    disconnectDirectLoadMoreObserver();
    disconnectDirectLoadMoreScrollHandler();

    if (
        !isMessagesListView()
        || loadMoreButton.hidden
        || loadMoreButton.disabled
        || !chatState.directRoomsHasMore
    ) {
        return;
    }

    if (document.body?.classList.contains("messages-page")) {
        const maybeLoadMore = () => {
            if (chatState.directRoomsLoading || !chatState.directRoomsHasMore) {
                return;
            }
            const rect = loadMoreButton.getBoundingClientRect();
            if (rect.top <= window.innerHeight + 220) {
                void handleDirectRoomsLoadMore();
            }
        };
        const onScroll = () => {
            if (chatState.directLoadMoreScrollFrame) {
                return;
            }
            chatState.directLoadMoreScrollFrame = window.requestAnimationFrame(() => {
                chatState.directLoadMoreScrollFrame = 0;
                maybeLoadMore();
            });
        };
        chatState.directLoadMoreScrollHandler = onScroll;
        window.addEventListener("scroll", onScroll, { passive: true });
        window.addEventListener("resize", onScroll, { passive: true });
        window.requestAnimationFrame(maybeLoadMore);
        return;
    }

    if (typeof window.IntersectionObserver !== "function") {
        return;
    }

    const observer = new window.IntersectionObserver((entries) => {
        if (!entries.some((entry) => entry.isIntersecting)) {
            return;
        }
        disconnectDirectLoadMoreObserver();
        if (chatState.directRoomsLoading || !chatState.directRoomsHasMore) {
            return;
        }
        void handleDirectRoomsLoadMore();
    }, {
        threshold: 0.01,
        rootMargin: "0px 0px 280px 0px"
    });

    chatState.directLoadMoreObserver = observer;
    observer.observe(loadMoreButton);
}

function syncChatViewUI() {
    const sidebarNode = document.querySelector(".chat-sidebar");
    const mainNode = document.querySelector(".chat-main");
    const publicSection = document.getElementById("chat-public-section");
    const directSection = document.getElementById("chat-direct-section");
    const publicButton = document.getElementById("chat-view-public-btn");
    const messagesButton = document.getElementById("chat-view-messages-btn");
    const introNode = document.getElementById("chat-sidebar-intro");
    const pageMode = chatPageMode();

    document.documentElement.setAttribute("data-chat-page-mode", pageMode);
    document.body.classList.toggle("messages-layout", pageMode === MESSAGES_VIEW);
    document.body.classList.toggle("public-chat-layout", pageMode === PUBLIC_CHAT_VIEW);
    document.body.classList.toggle("direct-focus-layout", pageMode === DIRECT_FOCUS_VIEW);
    if (pageMode !== MESSAGES_VIEW) {
        disconnectDirectLoadMoreObserver();
        disconnectDirectLoadMoreScrollHandler();
    }
    if (sidebarNode) {
        sidebarNode.hidden = pageMode !== MESSAGES_VIEW;
    }
    if (mainNode) {
        mainNode.hidden = pageMode === MESSAGES_VIEW;
    }
    if (publicSection) {
        publicSection.hidden = pageMode !== PUBLIC_CHAT_VIEW;
    }
    if (directSection) {
        directSection.hidden = pageMode !== MESSAGES_VIEW;
    }
    if (publicButton) {
        publicButton.hidden = true;
        publicButton.classList.toggle("is-active", pageMode === PUBLIC_CHAT_VIEW);
    }
    if (messagesButton) {
        messagesButton.hidden = true;
        messagesButton.classList.toggle("is-active", pageMode === MESSAGES_VIEW);
    }
    if (introNode) {
        introNode.innerHTML = pageMode === MESSAGES_VIEW
            ? 'Jen vaše soukromé konverzace. Nový chat otevřete přes <a href="/following.html">sledované houbaře</a>.'
            : pageMode === DIRECT_FOCUS_VIEW
                ? "Soukromá konverzace se otevře přímo bez seznamu dalších chatů."
                : `Tady se otevře jen veřejný chat. Uchovává se posledních ${PUBLIC_CHAT_HISTORY_LIMIT} zpráv.`;
    }
}

function renderPublicRoomList() {
    const container = document.getElementById("chat-public-rooms");
    if (!container) {
        return;
    }

    if (!chatState.publicRooms.length) {
        container.innerHTML = '<p class="muted-copy">Zatím nejsou dostupné žádné veřejné místnosti.</p>';
        return;
    }

    container.innerHTML = chatState.publicRooms.map((room) => {
        const activeClass = activeChatRoomId() === String(room.id) ? "is-active" : "";
        const unreadBadge = room.unread_count > 0
            ? `<span class="chat-unread-badge">${escapeHtml(String(room.unread_count))}</span>`
            : "";
        const dateLabel = room.last_message?.created_at
            ? formatDateTime(room.last_message.created_at)
            : formatDateTime(room.created_at);
        return `
            <button type="button" class="chat-room-item ${activeClass}" data-chat-room-id="${escapeHtml(String(room.id))}">
                <div class="chat-room-item-head">
                    <strong>${escapeHtml(roomDisplayTitle(room))}</strong>
                    ${unreadBadge}
                </div>
                <div class="chat-room-meta">
                    <span>${escapeHtml(dateLabel)}</span>
                    <span>${escapeHtml(roomDisplayMeta(room))}</span>
                </div>
            </button>
        `;
    }).join("");

    container.querySelectorAll("[data-chat-room-id]").forEach((button) => {
        button.addEventListener("click", async () => {
            const roomID = String(button.getAttribute("data-chat-room-id") || "").trim();
            const room = findKnownRoom(roomID);
            if (room) {
                await openRoom(room);
            }
        });
    });
}

function renderDirectRoomList() {
    const container = document.getElementById("chat-direct-rooms");
    const loadMoreButton = document.getElementById("chat-direct-load-more");
    if (!container) {
        disconnectDirectLoadMoreObserver();
        return;
    }

    if (!(chatState.session?.logged_in && chatState.me)) {
        container.innerHTML = `
            <p class="muted-copy">
                Do soukromých zpráv vstoupíte po přihlášení.
                <a href="${API_URL}/auth/login">Přihlásit se</a>
                nebo
                <a href="${buildAhoj420RegisterURL()}">vytvořit účet</a>.
            </p>
        `;
        if (loadMoreButton) {
            loadMoreButton.hidden = true;
            loadMoreButton.disabled = false;
        }
        syncDirectLoadMoreTrigger();
        return;
    }

    const rooms = sortedDirectRooms();
    if (!rooms.length) {
        container.innerHTML = `
            <p class="muted-copy">
                Zatím tu nemáte žádné konverzace.
                Začněte třeba přes <a href="/following.html">sledované houbaře</a>.
            </p>
        `;
        if (loadMoreButton) {
            loadMoreButton.hidden = true;
            loadMoreButton.disabled = false;
        }
        syncDirectLoadMoreTrigger();
        return;
    }

    container.innerHTML = rooms.map((room) => {
        const unreadBadge = room.unread_count > 0
            ? `<span class="chat-unread-badge">${escapeHtml(String(room.unread_count))}</span>`
            : "";
        const dateLabel = room.last_message?.created_at
            ? formatDateTime(room.last_message.created_at)
            : formatDateTime(room.created_at);
        const preview = roomPreviewSnippet(room, 20);
        const readHref = room.other_user_id > 0
            ? `/chat.html?dm=${encodeURIComponent(String(room.other_user_id))}`
            : "";
        return `
            <article class="chat-room-card">
                <div class="chat-room-item chat-room-item-with-avatar">
                    <div class="chat-room-item-head">
                        <span class="chat-room-user">
                            <img src="${escapeHtml(directRoomAvatar(room))}" alt="${escapeHtml(roomDisplayTitle(room))}" class="chat-room-avatar">
                            <strong>${escapeHtml(roomDisplayTitle(room))}</strong>
                        </span>
                        ${unreadBadge}
                    </div>
                    <div class="chat-room-meta">
                        <span class="chat-list-date">${escapeHtml(dateLabel)}</span>
                        <span class="chat-room-preview">${escapeHtml(preview)}</span>
                    </div>
                </div>
                <div class="chat-room-actions">
                    ${readHref
                        ? `<a href="${readHref}" class="btn btn-secondary">Číst</a>`
                        : `<button type="button" class="btn btn-secondary" data-chat-open-room="${escapeHtml(String(room.id))}">Číst</button>`
                    }
                    <button type="button" class="btn btn-secondary btn-danger-soft" data-chat-delete-room="${escapeHtml(String(room.id))}">Smazat</button>
                </div>
            </article>
        `;
    }).join("");

    container.querySelectorAll("[data-chat-open-room]").forEach((button) => {
        button.addEventListener("click", async () => {
            const roomID = String(
                button.getAttribute("data-chat-open-room")
                || ""
            ).trim();
            const room = findKnownRoom(roomID);
            if (room) {
                await openRoom(room);
            }
        });
    });

    container.querySelectorAll("[data-chat-delete-room]").forEach((button) => {
        button.addEventListener("click", async () => {
            const roomID = String(button.getAttribute("data-chat-delete-room") || "").trim();
            if (roomID) {
                await handleDirectRoomDelete(roomID);
            }
        });
    });

    if (loadMoreButton) {
        loadMoreButton.hidden = !chatState.directRoomsHasMore;
        loadMoreButton.disabled = chatState.directRoomsLoading;
    }
    syncDirectLoadMoreTrigger();
}

function renderCurrentSidebar() {
    syncChatViewUI();
    if (isMessagesListView()) {
        renderDirectRoomList();
        return;
    }
}

function activeRoomStatusText(room) {
    if (!room) {
        return isFocusedDirectView()
            ? "Načítám soukromou konverzaci."
            : isMessagesListView()
            ? "Vyberte konverzaci vlevo nebo otevřete novou zprávu ze stránky sledovaných houbařů."
            : `Veřejný chat uchovává jen posledních ${PUBLIC_CHAT_HISTORY_LIMIT} zpráv.`;
    }
    if (room.kind === "dm") {
        return "Soukromé zprávy vidí jen účastníci této konverzace. Moderace se na ně nevztahuje.";
    }
    return `Veřejná místnost. Uchovává se jen posledních ${PUBLIC_CHAT_HISTORY_LIMIT} zpráv. Moderace platí pouze pro veřejný chat.`;
}

function renderActiveRoom() {
    const titleNode = document.getElementById("chat-room-title");
    const eyebrowNode = document.getElementById("chat-room-eyebrow");
    const statusNode = document.getElementById("chat-room-status");
    const listNode = document.getElementById("chat-message-list");
    const form = document.getElementById("chat-message-form");
    const input = document.getElementById("chat-message-input");

    if (!titleNode || !eyebrowNode || !statusNode || !listNode || !form) {
        return;
    }

    if (!chatState.activeRoom) {
        eyebrowNode.textContent = isFocusedDirectView() ? "Soukromé zprávy" : (isMessagesListView() ? "Zprávy" : "Veřejný chat");
        titleNode.textContent = isFocusedDirectView() ? "Načítám konverzaci" : (isMessagesListView() ? "Vyberte konverzaci" : "Veřejný chat");
        statusNode.textContent = activeRoomStatusText(null);
        listNode.innerHTML = isFocusedDirectView()
            ? '<p class="muted-copy">Otevírám soukromou konverzaci…</p>'
            : isMessagesListView()
            ? '<p class="muted-copy">Po výběru konverzace se tu zobrazí historie zpráv.</p>'
            : '<p class="muted-copy">Načítám veřejný chat…</p>';
        form.hidden = true;
        return;
    }

    eyebrowNode.textContent = chatState.activeRoom.kind === "dm" ? "Soukromé zprávy" : "Veřejný chat";
    titleNode.textContent = roomDisplayTitle(chatState.activeRoom);
    statusNode.textContent = activeRoomStatusText(chatState.activeRoom);
    form.hidden = false;
    if (input) {
        input.placeholder = chatState.activeRoom.kind === "dm"
            ? "Napište soukromou zprávu"
            : "Napište zprávu do veřejného chatu";
    }

    if (!chatState.messages.length) {
        listNode.innerHTML = chatState.activeRoom.kind === "dm"
            ? '<p class="muted-copy">Zatím tu nejsou žádné soukromé zprávy.</p>'
            : '<p class="muted-copy">Zatím tu nejsou žádné zprávy.</p>';
        return;
    }

    const visibleMessages = [...chatState.messages].reverse();
    listNode.innerHTML = visibleMessages.map((message) => {
        const mine = Number(message.author_user_id) === Number(chatState.me?.id);
        const deleted = isDeletedMessage(message);
        const deleteAction = canDeleteMessage(message)
            ? `
                <button
                    type="button"
                    class="chat-message-delete"
                    data-chat-delete-message="${escapeHtml(String(message.id))}"
                >Smazat</button>
            `
            : "";
        return `
            <article class="chat-message-item ${mine ? "is-mine" : ""} ${deleted ? "is-deleted" : ""}">
                <div class="chat-message-head">
                    <span class="chat-message-author">
                        <img src="${escapeHtml(message.author_avatar || DEFAULT_AVATAR_URL)}" alt="${escapeHtml(message.author_name || "houbař")}">
                        <span>${escapeHtml(message.author_name || "Houbař")}</span>
                    </span>
                    <span class="chat-message-meta">
                        <span class="muted-copy">${escapeHtml(formatDateTime(message.created_at))}</span>
                        ${deleteAction}
                    </span>
                </div>
                <div class="chat-message-copy ${deleted ? "is-deleted" : ""}">${deleted ? escapeHtml(deletedMessageCopy()) : escapeMessageContent(message.content)}</div>
            </article>
        `;
    }).join("");

    listNode.querySelectorAll("[data-chat-delete-message]").forEach((button) => {
        button.addEventListener("click", async () => {
            const messageID = String(button.getAttribute("data-chat-delete-message") || "").trim();
            if (messageID) {
                await handleMessageDelete(messageID);
            }
        });
    });

    listNode.scrollTop = 0;
}

async function loadPublicRooms() {
    const payload = await chatJsonRequest("/api/chat/rooms/public");
    chatState.publicRooms = Array.isArray(payload?.rooms) ? payload.rooms : [];
    renderPublicRoomList();
}

async function loadDirectRooms() {
    return loadDirectRoomsPage({ reset: true });
}

async function loadDirectRoomsPage({ reset = false, limit = chatState.directRoomsLimit } = {}) {
    const requestLimit = Math.max(1, Number(limit || chatState.directRoomsLimit || 20));
    const requestOffset = reset ? 0 : chatState.directRoomsOffset;
    chatState.directRoomsLoading = true;
    renderDirectRoomList();

    try {
        const payload = await chatJsonRequest(`/api/chat/rooms/direct?limit=${requestLimit}&offset=${requestOffset}`);
        const rooms = Array.isArray(payload?.rooms) ? payload.rooms : [];
        chatState.directRooms = reset ? rooms : [...chatState.directRooms, ...rooms];
        chatState.directRoomsOffset = requestOffset + rooms.length;
        chatState.directRoomsHasMore = Boolean(payload?.has_more);
        chatState.directRoomsLimit = requestLimit;
        renderDirectRoomList();
        return payload;
    } finally {
        chatState.directRoomsLoading = false;
        renderDirectRoomList();
    }
}

async function handleDirectRoomsLoadMore() {
    if (chatState.directRoomsLoading || !chatState.directRoomsHasMore) {
        return;
    }
    try {
        await loadDirectRoomsPage({ reset: false, limit: chatState.directRoomsLimit });
    } catch (error) {
        console.error("Failed to load more direct rooms", error);
        showToast(error.message || "Konverzace se nepodařilo načíst.", { kind: "error" });
    }
}

async function refreshSidebar() {
    if (isMessagesListView()) {
        const loadedCount = Math.max(chatState.directRooms.length, chatState.directRoomsLimit);
        await loadDirectRoomsPage({ reset: true, limit: loadedCount });
        return;
    }
    if (isPublicChatView()) {
        await loadPublicRooms();
    }
}

async function refreshGlobalChatUnread() {
    if (typeof window.refreshHeaderChatUnreadCount === "function") {
        try {
            await window.refreshHeaderChatUnreadCount();
        } catch (_error) {
            // Ignore unread badge refresh errors on the page itself.
        }
    }
}

async function markActiveRoomRead() {
    if (!chatState.activeRoom || !chatState.messages.length) {
        return;
    }
    const lastMessage = chatState.messages[chatState.messages.length - 1];
    const hadUnread = [...chatState.publicRooms, ...chatState.directRooms].some((room) => (
        String(room.id) === String(chatState.activeRoom.id) && Number(room.unread_count || 0) > 0
    ));
    const shouldRefreshHeaderUnread = hadUnread || isFocusedDirectView();
    try {
        await chatJsonRequest(`/api/chat/rooms/${encodeURIComponent(chatState.activeRoom.id)}/read`, {
            method: "POST",
            body: { last_message_id: lastMessage.id }
        });
        const updateUnread = (room) => {
            if (String(room.id) === String(chatState.activeRoom.id)) {
                room.unread_count = 0;
            }
        };
        chatState.publicRooms.forEach(updateUnread);
        chatState.directRooms.forEach(updateUnread);
        renderCurrentSidebar();
        if (shouldRefreshHeaderUnread) {
            await refreshGlobalChatUnread();
        }
    } catch (error) {
        console.error("Failed to mark room as read", error);
    }
}

async function openRoom(room) {
    chatState.activeRoom = room;
    renderActiveRoom();

    const roomLimit = room.kind === "public" ? PUBLIC_CHAT_HISTORY_LIMIT : DIRECT_CHAT_HISTORY_LIMIT;
    const payload = await chatJsonRequest(`/api/chat/rooms/${encodeURIComponent(room.id)}/messages?limit=${roomLimit}`);
    chatState.activeRoom = payload?.room || room;
    chatState.messages = Array.isArray(payload?.messages) ? payload.messages : [];
    renderActiveRoom();
    await markActiveRoomRead();
}

async function openDirectRoom(targetUserID) {
    const payload = await chatJsonRequest("/api/chat/rooms/direct", {
        method: "POST",
        body: { target_user_id: targetUserID }
    });
    const room = payload?.room;
    if (!room) {
        throw new Error("Soukromou konverzaci se nepodařilo otevřít.");
    }
    if (chatPageMode() === PUBLIC_CHAT_VIEW) {
        updateChatViewURL(MESSAGES_VIEW, { replace: true });
    }
    if (!isFocusedDirectView()) {
        await loadDirectRooms();
    }
    const refreshedRoom = findKnownRoom(room.id) || room;
    await openRoom(refreshedRoom);
}

async function handleMessageSubmit(event) {
    event.preventDefault();
    if (!chatState.activeRoom) {
        return;
    }

    const input = document.getElementById("chat-message-input");
    const submit = document.getElementById("chat-message-submit");
    const statusNode = document.getElementById("chat-compose-status");
    const content = String(input?.value || "").trim();
    if (!content) {
        setStatusMessage(statusNode, "Zpráva nesmí být prázdná.", "error");
        return;
    }

    try {
        if (input) input.disabled = true;
        if (submit) submit.disabled = true;
        setStatusMessage(statusNode, "Odesílám zprávu...");

        const payload = await chatJsonRequest(`/api/chat/rooms/${encodeURIComponent(chatState.activeRoom.id)}/messages`, {
            method: "POST",
            body: { content }
        });
        if (input) {
            input.value = "";
            input.disabled = false;
        }
        if (submit) submit.disabled = false;
        setStatusMessage(statusNode, "", "");

        if (payload?.message) {
            chatState.messages.push(payload.message);
            renderActiveRoom();
        }
        await refreshSidebar();
        await markActiveRoomRead();
    } catch (error) {
        console.error("Failed to send chat message", error);
        if (input) input.disabled = false;
        if (submit) submit.disabled = false;
        setStatusMessage(statusNode, error.message || "Zprávu se nepodařilo odeslat.", "error");
    }
}

async function handleMessageDelete(messageID) {
    if (!chatState.activeRoom) {
        return;
    }

    const message = chatState.messages.find((item) => String(item.id) === String(messageID));
    if (!message || isDeletedMessage(message) || !canDeleteMessage(message)) {
        return;
    }

    const isOwnMessage = Number(message.author_user_id) === Number(chatState.me?.id);
    const confirmed = window.confirm(
        isOwnMessage
            ? "Opravdu chcete smazat tuto zprávu?"
            : "Opravdu chcete odstranit tuto zprávu z veřejného chatu jako moderátor?"
    );
    if (!confirmed) {
        return;
    }

    try {
        const payload = await chatJsonRequest(`/api/chat/rooms/${encodeURIComponent(chatState.activeRoom.id)}/messages/${encodeURIComponent(messageID)}`, {
            method: "DELETE"
        });
        if (payload?.message) {
            chatState.messages = chatState.messages.map((item) => (
                String(item.id) === String(messageID)
                    ? payload.message
                    : item
            ));
            renderActiveRoom();
        }
        await refreshSidebar();
        await refreshGlobalChatUnread();
        showToast("Zpráva byla smazána.", { kind: "success" });
    } catch (error) {
        console.error("Failed to delete chat message", error);
        showToast(error.message || "Zprávu se nepodařilo smazat.", { kind: "error" });
    }
}

async function handleDirectRoomDelete(roomID) {
    const room = chatState.directRooms.find((item) => String(item.id) === String(roomID));
    if (!room) {
        return;
    }

    const confirmed = window.confirm(`Opravdu chcete odebrat konverzaci s uživatelem ${roomDisplayTitle(room)} ze svého seznamu zpráv?`);
    if (!confirmed) {
        return;
    }

    try {
        await chatJsonRequest(`/api/chat/rooms/direct/${encodeURIComponent(roomID)}`, {
            method: "DELETE"
        });
        chatState.directRooms = chatState.directRooms.filter((item) => String(item.id) !== String(roomID));
        chatState.directRoomsOffset = Math.max(0, chatState.directRooms.length);
        if (String(chatState.activeRoom?.id || "") === String(roomID)) {
            chatState.activeRoom = null;
            chatState.messages = [];
        }
        renderDirectRoomList();
        renderActiveRoom();

        if (!chatState.activeRoom && !isMessagesListView() && chatState.directRooms.length) {
            await openRoom(sortedDirectRooms()[0]);
        }
        await refreshGlobalChatUnread();
        showToast("Konverzace byla odebrána z vašeho seznamu zpráv.", { kind: "success" });
    } catch (error) {
        console.error("Failed to delete direct room", error);
        showToast(error.message || "Konverzaci se nepodařilo smazat.", { kind: "error" });
    }
}

async function handleChatViewSwitch(view) {
    if (view !== PUBLIC_CHAT_VIEW && view !== MESSAGES_VIEW) {
        return;
    }
    if ((view === PUBLIC_CHAT_VIEW && isPublicChatView()) || (view === MESSAGES_VIEW && isMessagesView())) {
        return;
    }

    updateChatViewURL(view);
    chatState.activeRoom = null;
    chatState.messages = [];
    renderCurrentSidebar();
    renderActiveRoom();

    if (!chatState.session?.logged_in || !chatState.me) {
        showGuestChatState();
        return;
    }

    await refreshSidebar();
    await openInitialRoomForCurrentView();
}

function restartPolling() {
    if (chatState.pollingHandle) {
        window.clearInterval(chatState.pollingHandle);
    }
    chatState.pollingHandle = window.setInterval(async () => {
        if (chatState.pollingBusy) {
            return;
        }
        chatState.pollingBusy = true;
        try {
            await refreshSidebar();
            if (chatState.activeRoom) {
                const room = findKnownRoom(chatState.activeRoom.id) || chatState.activeRoom;
                await openRoom(room);
            }
        } catch (error) {
            console.error("Failed to poll chat state", error);
        } finally {
            chatState.pollingBusy = false;
        }
    }, 30000);
}

function showGuestChatState() {
    const listNode = document.getElementById("chat-message-list");
    const statusNode = document.getElementById("chat-room-status");
    const titleNode = document.getElementById("chat-room-title");
    const eyebrowNode = document.getElementById("chat-room-eyebrow");
    const form = document.getElementById("chat-message-form");

    syncChatViewUI();
    if (eyebrowNode) {
        eyebrowNode.textContent = isFocusedDirectView() ? "Soukromé zprávy" : (isMessagesListView() ? "Zprávy" : "Veřejný chat");
    }
    if (titleNode) {
        titleNode.textContent = isFocusedDirectView()
            ? "Přihlaste se do soukromé zprávy"
            : (isMessagesListView() ? "Přihlaste se do zpráv" : "Přihlaste se do chatu");
    }
    if (statusNode) {
        statusNode.textContent = isMessagesView()
            ? "Soukromé zprávy fungují přes samostatný chat-service, ale přihlášení zůstává přes hlavní účet Houbám Zdar."
            : `Veřejný chat funguje přes samostatný chat-service a uchovává jen posledních ${PUBLIC_CHAT_HISTORY_LIMIT} zpráv.`;
    }
    if (listNode) {
        listNode.innerHTML = `
            <p class="muted-copy">
                ${isMessagesView() ? "Do soukromých zpráv vstoupíte" : "Do veřejného chatu vstoupíte"} po přihlášení.
                <a href="${API_URL}/auth/login">Přihlásit se</a>
                nebo
                <a href="${buildAhoj420RegisterURL()}">vytvořit účet</a>.
            </p>
        `;
    }
    if (form) {
        form.hidden = true;
    }

    chatState.publicRooms = [];
    chatState.directRooms = [];
    renderCurrentSidebar();
}

async function openInitialRoomForCurrentView() {
    if (isFocusedDirectView()) {
        const requestedDirectUserID = pendingDirectUserID();
        if (requestedDirectUserID > 0 && requestedDirectUserID !== Number(chatState.me.id)) {
            await openDirectRoom(requestedDirectUserID);
            return;
        }
        renderActiveRoom();
        return;
    }

    if (isMessagesListView()) {
        chatState.activeRoom = null;
        chatState.messages = [];
        renderActiveRoom();
        return;
    }

    const initialRoom = chatState.publicRooms.find((room) => room.slug === "general") || chatState.publicRooms[0] || null;
    if (initialRoom) {
        await openRoom(initialRoom);
    } else {
        renderActiveRoom();
    }
}

async function initChatPage() {
    if (document.body.dataset.page !== "chat") {
        return;
    }

    const publicViewButton = document.getElementById("chat-view-public-btn");
    const messagesViewButton = document.getElementById("chat-view-messages-btn");
    const directLoadMoreButton = document.getElementById("chat-direct-load-more");
    publicViewButton?.addEventListener("click", () => {
        void handleChatViewSwitch(PUBLIC_CHAT_VIEW);
    });
    messagesViewButton?.addEventListener("click", () => {
        void handleChatViewSwitch(MESSAGES_VIEW);
    });
    directLoadMoreButton?.addEventListener("click", () => {
        void handleDirectRoomsLoadMore();
    });

    chatState.session = await apiGet("/api/session");
    if (chatState.session?.logged_in) {
        chatState.me = await apiGet("/api/me");
    }

    setAppIdentity(chatState.session, chatState.me);
    renderHeader(chatState.session, chatState.me);
    syncChatViewUI();
    renderActiveRoom();

    const messageForm = document.getElementById("chat-message-form");
    if (messageForm) {
        messageForm.addEventListener("submit", handleMessageSubmit);
    }

    if (!chatState.session?.logged_in || !chatState.me) {
        showGuestChatState();
        return;
    }

    try {
        await ensureChatToken(false);
        await refreshSidebar();
        await openInitialRoomForCurrentView();
        await refreshGlobalChatUnread();
        restartPolling();
    } catch (error) {
        console.error("Failed to initialize chat page", error);
        const listNode = document.getElementById("chat-message-list");
        const statusNode = document.getElementById("chat-room-status");
        const message = error?.message || "Chat se nepodařilo inicializovat.";
        if (statusNode) {
            statusNode.textContent = message;
        }
        if (listNode) {
            listNode.innerHTML = `<p class="muted-copy">${escapeHtml(message)}</p>`;
        }
    }
}

document.addEventListener("DOMContentLoaded", initChatPage);
