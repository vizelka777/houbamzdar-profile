export const API_URL = "https://api.houbamzdar.cz";

export const state = {
    isLoggedIn: false,
    user: null,
    profile: null,
    galleryPage: 1,
    galleryCaptures: [],
    galleryHasMore: true,
};

export async function apiGet(path) {
    try {
        const res = await fetch(`${API_URL}${path}`, {
            method: "GET",
            credentials: "include"
        });
        if (!res.ok) throw new Error(`HTTP error ${res.status}`);
        return await res.json();
    } catch (e) {
        console.error("API GET Error", e);
        return null;
    }
}

export async function apiPost(path, body = null) {
    try {
        const options = {
            method: "POST",
            credentials: "include",
            headers: {}
        };
        if (body) {
            options.headers["Content-Type"] = "application/json";
            options.body = JSON.stringify(body);
        }
        const res = await fetch(`${API_URL}${path}`, options);
        if (!res.ok) throw new Error(`HTTP error ${res.status}`);
        return await res.json();
    } catch (e) {
        console.error("API POST Error", e);
        return null;
    }
}

export async function checkAuth() {
    const session = await apiGet("/api/session");
    if (session && session.logged_in) {
        state.isLoggedIn = true;
        state.user = session.user;
        state.profile = await apiGet("/api/me");
    } else {
        state.isLoggedIn = false;
        state.user = null;
        state.profile = null;
    }
    document.dispatchEvent(new Event('auth-changed'));
}

export async function fetchGallery(page = 1, pageSize = 24, filters = {}) {
    const params = new URLSearchParams();
    params.set("page", String(page));
    params.set("page_size", String(pageSize));
    
    if (filters.species) params.set("species", filters.species);
    if (filters.kraj) params.set("kraj", filters.kraj);
    if (filters.sort) params.set("sort", filters.sort);
    
    return await apiGet(`/api/public/captures?${params.toString()}`);
}

export function login() {
    // Save current path to return back after login if needed
    const returnUrl = encodeURIComponent(window.location.href);
    window.location.href = `${API_URL}/auth/login?next=${returnUrl}`;
}

export async function logout() {
    const res = await apiPost("/auth/logout");
    state.isLoggedIn = false;
    state.user = null;
    state.profile = null;
    document.dispatchEvent(new Event('auth-changed'));
    
    if (res && res.idp_logout_url) {
        const alsoAhoj = window.confirm("Odhlásit se i z ahoj420.eu?");
        if (alsoAhoj) {
            window.location.href = res.idp_logout_url;
        } else {
            window.location.href = "/";
        }
    } else {
        window.location.href = "/";
    }
}
