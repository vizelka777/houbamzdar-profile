const CACHE_VERSION = "hzd-pwa-v2";
const STATIC_CACHE = `${CACHE_VERSION}-static`;
const RUNTIME_CACHE = `${CACHE_VERSION}-runtime`;
const OFFLINE_URL = "/offline.html";
const CDN_ORIGINS = new Set([
    "https://cdn.jsdelivr.net",
    "https://unpkg.com"
]);

const PRECACHE_URLS = [
    "/",
    "/index.html",
    "/news.html",
    "/info.html",
    "/feed.html",
    "/gallery.html",
    "/map.html",
    "/my-map.html",
    "/capture.html",
    "/create-post.html",
    "/edit-post.html",
    "/me.html",
    "/public-profile.html",
    "/following.html",
    "/users.html",
    "/messages.html",
    "/chat.html",
    "/register.html",
    "/reauth.html",
    "/admin.html",
    "/moderation.html",
    "/server-storage.html",
    "/faq.html",
    "/terms.html",
    "/privacy.html",
    "/cookies.html",
    "/rules.html",
    OFFLINE_URL,
    "/app.js",
    "/feed.js",
    "/news.js",
    "/gallery.js",
    "/map.js",
    "/my-map.js",
    "/capture.js",
    "/create-post.js",
    "/edit-post.js",
    "/users.js",
    "/chat.js",
    "/admin.js",
    "/moderation.js",
    "/server-storage.js",
    "/legal.js",
    "/profile-map.js",
    "/map-clusters.js",
    "/pwa.js",
    "/styles.css",
    "/styles/base.css",
    "/styles/page-admin.css",
    "/styles/page-capture.css",
    "/styles/page-chat.css",
    "/styles/page-editor.css",
    "/styles/page-explore.css",
    "/styles/page-feed.css",
    "/styles/page-gallery.css",
    "/styles/page-home.css",
    "/styles/page-legal.css",
    "/styles/page-map.css",
    "/styles/page-moderation.css",
    "/styles/page-news.css",
    "/styles/page-profile.css",
    "/vendor/leaflet-markercluster/MarkerCluster.css",
    "/vendor/leaflet-markercluster/leaflet.markercluster.js",
    "/favicon.ico",
    "/apple-touch-icon.png",
    "/logo.png",
    "/default-avatar.png",
    "/manifest.webmanifest",
    "/icon.svg",
    "/icon-maskable.svg",
    "/icon-app-192.png",
    "/icon-app-512.png",
    "/icon-maskable-192.png",
    "/icon-maskable-512.png"
];

self.addEventListener("install", (event) => {
    event.waitUntil(
        caches.open(STATIC_CACHE)
            .then((cache) => cache.addAll(PRECACHE_URLS))
            .then(() => self.skipWaiting())
    );
});

self.addEventListener("activate", (event) => {
    event.waitUntil((async () => {
        const keys = await caches.keys();
        await Promise.all(
            keys
                .filter((key) => key !== STATIC_CACHE && key !== RUNTIME_CACHE)
                .map((key) => caches.delete(key))
        );
        await self.clients.claim();
    })());
});

self.addEventListener("fetch", (event) => {
    const { request } = event;
    if (request.method !== "GET") {
        return;
    }

    const url = new URL(request.url);
    if (request.mode === "navigate") {
        event.respondWith(handleNavigationRequest(request, url));
        return;
    }

    if (url.origin === self.location.origin) {
        event.respondWith(handleStaticRequest(request));
        return;
    }

    if (CDN_ORIGINS.has(url.origin) && isCacheableAssetRequest(request)) {
        event.respondWith(handleRuntimeAssetRequest(request));
    }
});

async function handleNavigationRequest(request, url) {
    try {
        const response = await fetch(buildFreshRequest(request));
        if (response && response.ok) {
            const cache = await caches.open(RUNTIME_CACHE);
            cache.put(normalizeNavigationKey(url), response.clone());
        }
        return response;
    } catch (error) {
        const cached = await matchCachedNavigation(url);
        if (cached) {
            return cached;
        }
        return caches.match(OFFLINE_URL);
    }
}

async function handleStaticRequest(request) {
    const cacheKey = request;

    try {
        const response = await fetch(buildFreshRequest(request));
        if (response && response.ok) {
            const cache = await caches.open(RUNTIME_CACHE);
            cache.put(cacheKey, response.clone());
        }
        return response;
    } catch (error) {
        const cached = await caches.match(cacheKey);
        if (cached) {
            return cached;
        }

        if (request.destination === "document") {
            return caches.match(OFFLINE_URL);
        }

        return new Response("", {
            status: 504,
            statusText: "Gateway Timeout"
        });
    }
}

async function handleRuntimeAssetRequest(request) {
    const cached = await caches.match(request);
    if (cached) {
        return cached;
    }

    try {
        const response = await fetch(request);
        if (response && (response.ok || response.type === "opaque")) {
            const cache = await caches.open(RUNTIME_CACHE);
            cache.put(request, response.clone());
        }
        return response;
    } catch (error) {
        if (cached) {
            return cached;
        }
        return new Response("", {
            status: 504,
            statusText: "Gateway Timeout"
        });
    }
}

async function matchCachedNavigation(url) {
    const normalized = normalizeNavigationKey(url);
    return caches.match(normalized);
}

function normalizeNavigationKey(url) {
    const path = url.pathname === "" ? "/" : url.pathname;
    return new Request(path, { method: "GET" });
}

function isCacheableAssetRequest(request) {
    return ["style", "script", "image", "font"].includes(request.destination);
}

function buildFreshRequest(request) {
    return new Request(request, {
        cache: "no-cache"
    });
}
