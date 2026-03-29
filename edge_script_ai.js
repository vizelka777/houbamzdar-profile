import * as BunnySDK from "@bunny.net/edgescript-sdk";
import { createClient } from "@libsql/client/web";
import process from "node:process";

const SCRIPT_NAME = "houbamzdar-site-assistant";
const DEFAULT_ALLOWED_ORIGIN = readEnv("ALLOWED_ORIGIN") || "https://houbamzdar.cz";
const DEFAULT_SITE_ORIGIN = readEnv("SITE_ORIGIN") || "https://houbamzdar.cz";
const DEFAULT_MODEL = readEnv("OPENAI_MODEL") || "gpt-5-mini";
const OPENAI_API_URL = readEnv("OPENAI_API_URL") || "https://api.openai.com/v1/responses";
const OPENAI_API_KEY = readEnv("OPENAI_API_KEY");
const ASSISTANT_NAME = readEnv("ASSISTANT_NAME") || "Houbam Zdar Assistant";
const BUNNY_DATABASE_URL = readEnv("BUNNY_DATABASE_URL");
const BUNNY_DATABASE_AUTH_TOKEN = readEnv("BUNNY_DATABASE_AUTH_TOKEN");
const MAX_INPUT_CHARS = clampInteger(readEnv("MAX_INPUT_CHARS"), 2000, 200, 4000);
const MAX_HISTORY_ITEMS = clampInteger(readEnv("MAX_HISTORY_ITEMS"), 6, 0, 12);
const MAX_THREAD_MESSAGES = clampInteger(readEnv("MAX_THREAD_MESSAGES"), 20, 6, 40);
const MAX_REQUEST_CHARS = 12000;

const COMMON_HEADERS = {
    "content-type": "application/json; charset=utf-8",
    "cache-control": "no-store",
    "x-content-type-options": "nosniff"
};

const assistantDbState = {
    client: null,
    setupPromise: null
};

class HttpError extends Error {
    constructor(status, message) {
        super(message);
        this.name = "HttpError";
        this.status = status;
    }
}

function readEnv(name) {
    return String(process.env[name] || "").trim();
}

function clampInteger(raw, fallback, min, max) {
    const value = Number.parseInt(String(raw || ""), 10);
    if (!Number.isFinite(value)) {
        return fallback;
    }
    return Math.max(min, Math.min(max, value));
}

function nowIso() {
    return new Date().toISOString();
}

function createId() {
    if (typeof crypto !== "undefined" && typeof crypto.randomUUID === "function") {
        return crypto.randomUUID();
    }
    return `${Date.now()}-${Math.random().toString(16).slice(2)}-${Math.random().toString(16).slice(2)}`;
}

function resolveCorsOrigin(request) {
    const requestOrigin = String(request.headers.get("origin") || "").trim();
    if (!requestOrigin) {
        return DEFAULT_ALLOWED_ORIGIN;
    }
    return requestOrigin === DEFAULT_ALLOWED_ORIGIN ? requestOrigin : "";
}

function buildHeaders(request, extraHeaders = {}) {
    const headers = new Headers(COMMON_HEADERS);
    const corsOrigin = resolveCorsOrigin(request);

    if (corsOrigin) {
        headers.set("access-control-allow-origin", corsOrigin);
    }
    headers.set("access-control-allow-methods", "GET, POST, OPTIONS");
    headers.set("access-control-allow-headers", "Content-Type");
    headers.set("access-control-max-age", "86400");
    headers.set("vary", "Origin");

    Object.entries(extraHeaders).forEach(([key, value]) => {
        headers.set(key, value);
    });
    return headers;
}

function json(request, data, status = 200, extraHeaders = {}) {
    return new Response(JSON.stringify(data), {
        status,
        headers: buildHeaders(request, extraHeaders)
    });
}

function ensureAllowedOrigin(request) {
    const requestOrigin = String(request.headers.get("origin") || "").trim();
    if (requestOrigin && requestOrigin !== DEFAULT_ALLOWED_ORIGIN) {
        throw new HttpError(403, "Origin not allowed");
    }
}

async function readJsonBody(request) {
    const contentType = String(request.headers.get("content-type") || "").toLowerCase();
    if (!contentType.includes("application/json")) {
        throw new HttpError(415, "Expected application/json");
    }

    const rawBody = await request.text();
    if (!rawBody.trim()) {
        throw new HttpError(400, "Request body is required");
    }
    if (rawBody.length > MAX_REQUEST_CHARS) {
        throw new HttpError(413, "Request body is too large");
    }

    try {
        return JSON.parse(rawBody);
    } catch (_error) {
        throw new HttpError(400, "Invalid JSON");
    }
}

function normalizeSingleLineText(value, maxChars) {
    const text = String(value || "").trim().replace(/\s+/g, " ");
    if (!text) {
        return "";
    }
    return text.slice(0, maxChars);
}

function normalizeMessageText(value, maxChars = MAX_INPUT_CHARS) {
    const text = String(value || "")
        .replace(/\r\n?/g, "\n")
        .trim()
        .replace(/[ \t]+\n/g, "\n")
        .replace(/\n{3,}/g, "\n\n");

    if (!text) {
        return "";
    }
    return text.slice(0, maxChars);
}

function normalizeHistory(items) {
    if (!Array.isArray(items) || MAX_HISTORY_ITEMS <= 0) {
        return [];
    }

    return items
        .slice(-MAX_HISTORY_ITEMS)
        .map((item) => ({
            role: item?.role === "assistant" ? "assistant" : "user",
            content: normalizeMessageText(item?.content, MAX_INPUT_CHARS)
        }))
        .filter((item) => item.content);
}

function normalizeClientId(value) {
    const clientId = normalizeSingleLineText(value, 120);
    if (!clientId) {
        return "";
    }
    return /^[a-zA-Z0-9_-]{8,120}$/.test(clientId) ? clientId : "";
}

function normalizeThreadId(value) {
    const threadId = normalizeSingleLineText(value, 120);
    if (!threadId) {
        return "";
    }
    return /^[a-zA-Z0-9-]{8,120}$/.test(threadId) ? threadId : "";
}

function normalizeVote(value) {
    const vote = normalizeSingleLineText(value, 10).toLowerCase();
    return vote === "up" || vote === "down" ? vote : "";
}

function buildInstructions({ page = "", locale = "" } = {}) {
    const pageHint = normalizeSingleLineText(page, 160);
    const localeHint = normalizeSingleLineText(locale, 24);

    return [
        `You are ${ASSISTANT_NAME}, a support assistant for ${DEFAULT_SITE_ORIGIN}.`,
        "Help only with how the Houbam Zdar website works and how users should use it.",
        "Answer in the same language as the user unless they explicitly ask for another language.",
        "Be practical and concise. Prefer short numbered steps when explaining actions.",
        "Do not invent features, settings, policies, or account state.",
        "If the user asks about their personal account state, tell them you cannot inspect their account yet.",
        "If you are not sure, say that clearly and suggest the most likely next step on the site.",
        "Useful product facts: users register and sign in using the supported providers shown on the sign-in page; if they use a shared computer, tell them to use private mode and log out afterwards; to add a new device, they sign in on the new device using the same account method; the site includes a public chat and direct messages; houbičky are the app points shown in profile; better mushroom photos use daylight, sharp close shots, multiple angles, and visible habitat.",
        pageHint ? `Current page context: ${pageHint}.` : "",
        localeHint ? `Requested locale hint: ${localeHint}.` : ""
    ].filter(Boolean).join("\n");
}

function buildResponseInput(message, history) {
    const input = [];

    history.forEach((item) => {
        input.push({
            type: "message",
            role: item.role,
            content: item.content
        });
    });

    input.push({
        type: "message",
        role: "user",
        content: message
    });

    return input;
}

function extractOutputText(payload) {
    if (typeof payload?.output_text === "string" && payload.output_text.trim()) {
        return payload.output_text.trim();
    }

    if (!Array.isArray(payload?.output)) {
        return "";
    }

    const parts = [];
    payload.output.forEach((item) => {
        if (item?.type !== "message" || !Array.isArray(item?.content)) {
            return;
        }

        item.content.forEach((content) => {
            if (content?.type === "output_text" && typeof content?.text === "string") {
                parts.push(content.text);
                return;
            }
            if (content?.type === "text" && typeof content?.text === "string") {
                parts.push(content.text);
                return;
            }
            if (content?.type === "output_text" && typeof content?.text?.value === "string") {
                parts.push(content.text.value);
                return;
            }
            if (content?.type === "refusal" && typeof content?.refusal === "string") {
                parts.push(content.refusal);
            }
        });
    });

    return parts.join("\n\n").trim();
}

function isDatabaseConfigured() {
    return Boolean(BUNNY_DATABASE_URL && BUNNY_DATABASE_AUTH_TOKEN);
}

function getAssistantDbClient() {
    if (!isDatabaseConfigured()) {
        return null;
    }

    if (!assistantDbState.client) {
        assistantDbState.client = createClient({
            url: BUNNY_DATABASE_URL,
            authToken: BUNNY_DATABASE_AUTH_TOKEN
        });
    }

    return assistantDbState.client;
}

async function ensureAssistantDatabase() {
    if (!isDatabaseConfigured()) {
        return false;
    }

    if (!assistantDbState.setupPromise) {
        assistantDbState.setupPromise = (async () => {
            const client = getAssistantDbClient();
            if (!client) {
                throw new Error("Assistant database client is not available");
            }

            await client.execute("PRAGMA foreign_keys = ON");
            await client.execute(`
                CREATE TABLE IF NOT EXISTS assistant_threads (
                    id TEXT PRIMARY KEY,
                    client_id TEXT NOT NULL,
                    origin TEXT,
                    page_context TEXT,
                    locale TEXT,
                    status TEXT NOT NULL DEFAULT 'open',
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL,
                    last_message_at TEXT
                )
            `);
            await client.execute(`
                CREATE INDEX IF NOT EXISTS assistant_threads_client_updated_idx
                ON assistant_threads (client_id, updated_at DESC)
            `);
            await client.execute(`
                CREATE TABLE IF NOT EXISTS assistant_messages (
                    id TEXT PRIMARY KEY,
                    thread_id TEXT NOT NULL,
                    role TEXT NOT NULL,
                    content TEXT NOT NULL,
                    model TEXT,
                    response_id TEXT,
                    created_at TEXT NOT NULL,
                    FOREIGN KEY(thread_id) REFERENCES assistant_threads(id) ON DELETE CASCADE
                )
            `);
            await client.execute(`
                CREATE INDEX IF NOT EXISTS assistant_messages_thread_created_idx
                ON assistant_messages (thread_id, created_at ASC)
            `);
            await client.execute(`
                CREATE TABLE IF NOT EXISTS assistant_feedback (
                    id TEXT PRIMARY KEY,
                    thread_id TEXT NOT NULL,
                    message_id TEXT NOT NULL,
                    client_id TEXT NOT NULL,
                    vote TEXT NOT NULL,
                    note TEXT,
                    created_at TEXT NOT NULL,
                    updated_at TEXT NOT NULL,
                    UNIQUE(client_id, message_id)
                )
            `);
            await client.execute(`
                CREATE INDEX IF NOT EXISTS assistant_feedback_thread_idx
                ON assistant_feedback (thread_id, message_id)
            `);
            await client.execute(`
                CREATE TABLE IF NOT EXISTS assistant_events (
                    id TEXT PRIMARY KEY,
                    thread_id TEXT,
                    client_id TEXT NOT NULL,
                    event_type TEXT NOT NULL,
                    path TEXT,
                    payload_json TEXT,
                    created_at TEXT NOT NULL
                )
            `);
            await client.execute(`
                CREATE INDEX IF NOT EXISTS assistant_events_thread_created_idx
                ON assistant_events (thread_id, created_at DESC)
            `);
        })();
    }

    await assistantDbState.setupPromise;
    return true;
}

function safeJsonStringify(value) {
    try {
        return JSON.stringify(value ?? {});
    } catch (_error) {
        return "{}";
    }
}

async function recordAssistantEvent({ threadId = "", clientId = "", eventType = "", path = "", payload = null } = {}) {
    if (!isDatabaseConfigured()) {
        return;
    }

    const client = getAssistantDbClient();
    if (!client || !clientId || !eventType) {
        return;
    }

    try {
        await client.execute({
            sql: `
                INSERT INTO assistant_events (id, thread_id, client_id, event_type, path, payload_json, created_at)
                VALUES (?, ?, ?, ?, ?, ?, ?)
            `,
            args: [
                createId(),
                threadId || null,
                clientId,
                eventType,
                path || null,
                safeJsonStringify(payload),
                nowIso()
            ]
        });
    } catch (_error) {
        // Non-critical analytics should not break the assistant.
    }
}

async function createAssistantThread({ clientId = "", origin = "", page = "", locale = "" } = {}) {
    const client = getAssistantDbClient();
    if (!client) {
        throw new HttpError(503, "Assistant database is not configured");
    }

    const threadId = createId();
    const timestamp = nowIso();
    await client.execute({
        sql: `
            INSERT INTO assistant_threads (id, client_id, origin, page_context, locale, status, created_at, updated_at, last_message_at)
            VALUES (?, ?, ?, ?, ?, 'open', ?, ?, ?)
        `,
        args: [
            threadId,
            clientId,
            origin || null,
            page || null,
            locale || null,
            timestamp,
            timestamp,
            null
        ]
    });

    return {
        id: threadId
    };
}

async function getOwnedThread(threadId, clientId) {
    const client = getAssistantDbClient();
    if (!client) {
        return null;
    }

    const result = await client.execute({
        sql: `
            SELECT id, client_id, page_context, locale, created_at, updated_at
            FROM assistant_threads
            WHERE id = ? AND client_id = ?
            LIMIT 1
        `,
        args: [threadId, clientId]
    });

    return result.rows?.[0] || null;
}

async function resolveAssistantThread({ threadId = "", clientId = "", origin = "", page = "", locale = "" } = {}) {
    const normalizedThreadId = normalizeThreadId(threadId);
    if (normalizedThreadId) {
        const existing = await getOwnedThread(normalizedThreadId, clientId);
        if (existing) {
            return existing;
        }
    }

    return createAssistantThread({ clientId, origin, page, locale });
}

async function touchAssistantThread(threadId, { page = "", locale = "", hasMessage = false } = {}) {
    const client = getAssistantDbClient();
    if (!client || !threadId) {
        return;
    }

    const timestamp = nowIso();
    await client.execute({
        sql: `
            UPDATE assistant_threads
            SET page_context = COALESCE(?, page_context),
                locale = COALESCE(?, locale),
                updated_at = ?,
                last_message_at = CASE WHEN ? THEN ? ELSE last_message_at END
            WHERE id = ?
        `,
        args: [
            page || null,
            locale || null,
            timestamp,
            hasMessage ? 1 : 0,
            hasMessage ? timestamp : null,
            threadId
        ]
    });
}

async function insertAssistantMessage({ messageId = "", threadId = "", role = "", content = "", model = "", responseId = "" } = {}) {
    const client = getAssistantDbClient();
    if (!client) {
        return;
    }

    await client.execute({
        sql: `
            INSERT INTO assistant_messages (id, thread_id, role, content, model, response_id, created_at)
            VALUES (?, ?, ?, ?, ?, ?, ?)
        `,
        args: [
            messageId,
            threadId,
            role,
            content,
            model || null,
            responseId || null,
            nowIso()
        ]
    });
}

async function listAssistantMessages({ threadId = "", clientId = "", limit = MAX_THREAD_MESSAGES } = {}) {
    const client = getAssistantDbClient();
    if (!client) {
        return [];
    }

    const thread = await getOwnedThread(threadId, clientId);
    if (!thread) {
        return [];
    }

    const result = await client.execute({
        sql: `
            SELECT
                m.id,
                m.role,
                m.content,
                m.model,
                m.response_id,
                m.created_at,
                COALESCE(f.vote, '') AS feedback_vote
            FROM assistant_messages m
            LEFT JOIN assistant_feedback f
                ON f.message_id = m.id
               AND f.thread_id = m.thread_id
               AND f.client_id = ?
            WHERE m.thread_id = ?
            ORDER BY m.created_at ASC
            LIMIT ?
        `,
        args: [clientId, threadId, limit]
    });

    return (result.rows || []).map((row) => ({
        id: String(row.id || ""),
        role: String(row.role || "assistant"),
        content: String(row.content || ""),
        model: String(row.model || ""),
        response_id: String(row.response_id || ""),
        created_at: String(row.created_at || ""),
        feedback_vote: String(row.feedback_vote || "")
    }));
}

async function saveAssistantFeedback({ threadId = "", clientId = "", messageId = "", vote = "", note = "" } = {}) {
    const client = getAssistantDbClient();
    if (!client) {
        throw new HttpError(503, "Assistant database is not configured");
    }

    const thread = await getOwnedThread(threadId, clientId);
    if (!thread) {
        throw new HttpError(404, "Thread not found");
    }

    const messageResult = await client.execute({
        sql: `
            SELECT id
            FROM assistant_messages
            WHERE id = ? AND thread_id = ? AND role = 'assistant'
            LIMIT 1
        `,
        args: [messageId, threadId]
    });

    if (!messageResult.rows?.[0]) {
        throw new HttpError(404, "Assistant message not found");
    }

    const timestamp = nowIso();
    await client.execute({
        sql: `
            INSERT INTO assistant_feedback (id, thread_id, message_id, client_id, vote, note, created_at, updated_at)
            VALUES (?, ?, ?, ?, ?, ?, ?, ?)
            ON CONFLICT(client_id, message_id)
            DO UPDATE SET
                vote = excluded.vote,
                note = excluded.note,
                updated_at = excluded.updated_at
        `,
        args: [
            createId(),
            threadId,
            messageId,
            clientId,
            vote,
            note || null,
            timestamp,
            timestamp
        ]
    });

    await recordAssistantEvent({
        threadId,
        clientId,
        eventType: "feedback",
        payload: { message_id: messageId, vote }
    });

    return {
        ok: true,
        vote
    };
}

function buildPublicConfig() {
    return {
        ok: true,
        script: SCRIPT_NAME,
        configured: Boolean(OPENAI_API_KEY),
        db_configured: isDatabaseConfigured(),
        persistence: isDatabaseConfigured() ? "database" : "local",
        allowed_origin: DEFAULT_ALLOWED_ORIGIN,
        site_origin: DEFAULT_SITE_ORIGIN,
        model: DEFAULT_MODEL,
        max_input_chars: MAX_INPUT_CHARS,
        max_history_items: MAX_HISTORY_ITEMS,
        max_thread_messages: MAX_THREAD_MESSAGES,
        routes: {
            health: "/health",
            config: "/config",
            thread: "/thread",
            history: "/history",
            ask: "/ask",
            feedback: "/feedback"
        },
        missing: [
            ...(!OPENAI_API_KEY ? ["OPENAI_API_KEY"] : []),
            ...(isDatabaseConfigured() ? [] : ["BUNNY_DATABASE_URL", "BUNNY_DATABASE_AUTH_TOKEN"])
        ]
    };
}

async function createModelResponse({ message = "", history = [], page = "", locale = "" } = {}) {
    if (!OPENAI_API_KEY) {
        throw new HttpError(503, "OPENAI_API_KEY is not configured");
    }

    const response = await fetch(OPENAI_API_URL, {
        method: "POST",
        headers: {
            authorization: `Bearer ${OPENAI_API_KEY}`,
            "content-type": "application/json"
        },
        body: JSON.stringify({
            model: DEFAULT_MODEL,
            instructions: buildInstructions({ page, locale }),
            input: buildResponseInput(message, history),
            max_output_tokens: 800,
            store: false,
            reasoning: {
                effort: "low"
            },
            text: {
                format: {
                    type: "text"
                },
                verbosity: "low"
            }
        })
    });

    const payload = await response.json().catch(() => ({}));
    if (!response.ok) {
        const apiError = String(payload?.error?.message || "").trim();
        throw new HttpError(502, apiError || `Model request failed with status ${response.status}`);
    }

    const answer = extractOutputText(payload);
    if (!answer) {
        throw new HttpError(502, "Model returned no text output");
    }

    return {
        ok: true,
        answer,
        model: DEFAULT_MODEL,
        response_id: payload?.id || null
    };
}

async function handleCreateThread(request) {
    if (!isDatabaseConfigured()) {
        return json(request, { ok: false, error: "Assistant database is not configured" }, 503);
    }

    await ensureAssistantDatabase();
    const body = await readJsonBody(request);
    const clientId = normalizeClientId(body?.client_id);
    if (!clientId) {
        return json(request, { ok: false, error: "client_id is required" }, 400);
    }

    const page = normalizeSingleLineText(body?.page, 160);
    const locale = normalizeSingleLineText(body?.locale, 24);
    const origin = String(request.headers.get("origin") || "").trim();
    const thread = await createAssistantThread({ clientId, origin, page, locale });

    await recordAssistantEvent({
        threadId: thread.id,
        clientId,
        eventType: "thread_created",
        path: request.url,
        payload: { page, locale }
    });

    return json(request, {
        ok: true,
        thread_id: thread.id,
        messages: []
    });
}

async function handleLoadHistory(request) {
    if (!isDatabaseConfigured()) {
        return json(request, {
            ok: true,
            persistence: "local",
            thread_id: null,
            messages: []
        });
    }

    await ensureAssistantDatabase();
    const body = await readJsonBody(request);
    const clientId = normalizeClientId(body?.client_id);
    const threadId = normalizeThreadId(body?.thread_id);
    if (!clientId || !threadId) {
        return json(request, { ok: false, error: "thread_id and client_id are required" }, 400);
    }

    const messages = await listAssistantMessages({ threadId, clientId });
    if (!messages.length) {
        return json(request, {
            ok: true,
            persistence: "database",
            thread_id: threadId,
            messages: []
        });
    }

    await recordAssistantEvent({
        threadId,
        clientId,
        eventType: "history_loaded",
        path: request.url,
        payload: { count: messages.length }
    });

    return json(request, {
        ok: true,
        persistence: "database",
        thread_id: threadId,
        messages
    });
}

async function handleAsk(request) {
    const body = await readJsonBody(request);
    const message = normalizeMessageText(body?.message, MAX_INPUT_CHARS);
    const page = normalizeSingleLineText(body?.page, 160);
    const locale = normalizeSingleLineText(body?.locale, 24);

    if (!message) {
        return json(request, { ok: false, error: "Message is required" }, 400);
    }

    const dbEnabled = isDatabaseConfigured();
    let threadId = "";
    let clientId = "";
    let conversationHistory = [];

    if (dbEnabled) {
        await ensureAssistantDatabase();
        clientId = normalizeClientId(body?.client_id);
        if (!clientId) {
            return json(request, { ok: false, error: "client_id is required" }, 400);
        }

        const origin = String(request.headers.get("origin") || "").trim();
        const thread = await resolveAssistantThread({
            threadId: body?.thread_id,
            clientId,
            origin,
            page,
            locale
        });
        threadId = String(thread.id || "");

        const existingMessages = await listAssistantMessages({
            threadId,
            clientId,
            limit: MAX_THREAD_MESSAGES
        });
        conversationHistory = existingMessages
            .filter((item) => item.role === "user" || item.role === "assistant")
            .slice(-MAX_HISTORY_ITEMS)
            .map((item) => ({
                role: item.role === "assistant" ? "assistant" : "user",
                content: normalizeMessageText(item.content, MAX_INPUT_CHARS)
            }))
            .filter((item) => item.content);

        const userMessageId = createId();
        await insertAssistantMessage({
            messageId: userMessageId,
            threadId,
            role: "user",
            content: message
        });
        await touchAssistantThread(threadId, { page, locale, hasMessage: true });
        await recordAssistantEvent({
            threadId,
            clientId,
            eventType: "user_message",
            path: request.url,
            payload: { page, locale }
        });
    } else {
        conversationHistory = normalizeHistory(body?.history);
    }

    const result = await createModelResponse({
        message,
        history: conversationHistory,
        page,
        locale
    });

    let assistantMessageId = "";
    if (dbEnabled && threadId && clientId) {
        assistantMessageId = createId();
        await insertAssistantMessage({
            messageId: assistantMessageId,
            threadId,
            role: "assistant",
            content: result.answer,
            model: result.model,
            responseId: result.response_id || ""
        });
        await touchAssistantThread(threadId, { page, locale, hasMessage: true });
        await recordAssistantEvent({
            threadId,
            clientId,
            eventType: "assistant_message",
            path: request.url,
            payload: { response_id: result.response_id || null }
        });
    }

    return json(request, {
        ok: true,
        persistence: dbEnabled ? "database" : "local",
        thread_id: threadId || null,
        assistant_message_id: assistantMessageId || null,
        answer: result.answer,
        model: result.model,
        response_id: result.response_id || null
    });
}

async function handleFeedback(request) {
    if (!isDatabaseConfigured()) {
        return json(request, { ok: false, error: "Assistant database is not configured" }, 503);
    }

    await ensureAssistantDatabase();
    const body = await readJsonBody(request);
    const clientId = normalizeClientId(body?.client_id);
    const threadId = normalizeThreadId(body?.thread_id);
    const messageId = normalizeThreadId(body?.message_id);
    const vote = normalizeVote(body?.vote);
    const note = normalizeMessageText(body?.note, 500);

    if (!clientId || !threadId || !messageId || !vote) {
        return json(request, { ok: false, error: "client_id, thread_id, message_id and vote are required" }, 400);
    }

    const result = await saveAssistantFeedback({
        threadId,
        clientId,
        messageId,
        vote,
        note
    });

    return json(request, result);
}

BunnySDK.net.http.serve(async (request) => {
    const url = new URL(request.url);

    try {
        ensureAllowedOrigin(request);

        if (request.method === "OPTIONS") {
            return new Response(null, {
                status: 204,
                headers: buildHeaders(request)
            });
        }

        if (request.method === "GET" && (url.pathname === "/" || url.pathname === "" || url.pathname === "/config")) {
            return json(request, buildPublicConfig());
        }

        if (request.method === "GET" && url.pathname === "/health") {
            return json(request, {
                ok: true,
                script: SCRIPT_NAME,
                configured: Boolean(OPENAI_API_KEY),
                db_configured: isDatabaseConfigured()
            });
        }

        if (request.method === "POST" && url.pathname === "/thread") {
            return await handleCreateThread(request);
        }

        if (request.method === "POST" && url.pathname === "/history") {
            return await handleLoadHistory(request);
        }

        if (request.method === "POST" && url.pathname === "/ask") {
            return await handleAsk(request);
        }

        if (request.method === "POST" && url.pathname === "/feedback") {
            return await handleFeedback(request);
        }

        return json(request, { ok: false, error: "Not found" }, 404);
    } catch (error) {
        if (error instanceof HttpError) {
            return json(request, {
                ok: false,
                error: error.message
            }, error.status);
        }

        return json(request, {
            ok: false,
            error: error instanceof Error ? error.message : "Unexpected assistant error"
        }, 500);
    }
});
