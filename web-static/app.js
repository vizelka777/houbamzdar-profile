const API_URL = "https://api.houbamzdar.cz";

function escapeHtml(unsafe) {
    if (!unsafe) return '';
    return unsafe
         .toString()
         .replace(/&/g, "&amp;")
         .replace(/</g, "&lt;")
         .replace(/>/g, "&gt;")
         .replace(/"/g, "&quot;")
         .replace(/'/g, "&#039;");
}

async function apiGet(path) {
    try {
        const res = await fetch(`${API_URL}${path}`, {
            method: 'GET',
            credentials: 'include'
        });
        if (!res.ok) throw new Error(`HTTP error ${res.status}`);
        return await res.json();
    } catch (e) {
        console.error("API GET Error", e);
        return null;
    }
}

async function apiPost(path, body = null) {
    try {
        const options = {
            method: 'POST',
            credentials: 'include',
            headers: {}
        };
        if (body) {
            options.headers['Content-Type'] = 'application/json';
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

function renderHeader(session) {
    const authButtons = document.getElementById('auth-buttons');
    if (!authButtons) return;

    if (session && session.logged_in) {
        authButtons.innerHTML = `
            <span class="user-greeting">Привет, ${escapeHtml(session.user?.preferred_username || 'Гость')}</span>
            <button onclick="window.location.href='/me.html'" class="btn-primary">Личная страница</button>
            <button onclick="logoutFlow()" class="btn-secondary">Выход</button>
        `;
    } else {
        authButtons.innerHTML = `
            <button onclick="window.location.href='${API_URL}/auth/login'" class="btn-primary">Логин / Регистрация</button>
        `;
    }
}

async function logoutFlow() {
    const res = await apiPost('/auth/logout');
    if (res && res.idp_logout_url) {
        const alsoAhoj = confirm('Выйти также из ahoj420.eu?');
        if (alsoAhoj) {
            window.location.href = res.idp_logout_url;
        } else {
            window.location.href = '/';
        }
    } else {
        window.location.href = '/';
    }
}

async function initIndexPage() {
    const session = await apiGet('/api/session');
    renderHeader(session);
}

async function initMePage() {
    const session = await apiGet('/api/session');
    if (!session || !session.logged_in) {
        window.location.href = '/';
        return;
    }
    renderHeader(session);

    const me = await apiGet('/api/me');
    if (me) {
        if (me.picture) {
            document.getElementById('profile-picture').innerHTML = `<img src="${escapeHtml(me.picture)}" alt="Аватар">`;
        }
        
        document.getElementById('prof-username').value = me.preferred_username || '';
        document.getElementById('prof-email').value = me.email || '';
        document.getElementById('prof-phone').value = me.phone_number || '';
        
        const emStatus = document.getElementById('prof-email-status');
        if (me.email) {
            emStatus.textContent = me.email_verified ? '✓ подтверждён' : 'не подтверждён';
            emStatus.className = me.email_verified ? 'status-badge verified' : 'status-badge unverified';
        }
        
        const phStatus = document.getElementById('prof-phone-status');
        if (me.phone_number) {
            phStatus.textContent = me.phone_number_verified ? '✓ подтверждён' : 'не подтверждён';
            phStatus.className = me.phone_number_verified ? 'status-badge verified' : 'status-badge unverified';
        }

        document.getElementById('about-me-input').value = me.about_me || '';
    }

    const saveBtn = document.getElementById('save-about-btn');
    saveBtn.addEventListener('click', async () => {
        const aboutMeVal = document.getElementById('about-me-input').value;
        const saveStatus = document.getElementById('save-status');
        saveStatus.textContent = 'Сохранение...';
        
        const res = await apiPost('/api/me/about', { about_me: aboutMeVal });
        if (res && res.ok) {
            saveStatus.textContent = 'Сохранено';
            saveStatus.style.color = 'green';
            setTimeout(() => { saveStatus.textContent = ''; }, 3000);
        } else {
            saveStatus.textContent = 'Ошибка сохранения';
            saveStatus.style.color = 'red';
        }
    });
}