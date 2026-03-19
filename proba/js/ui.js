import { state, login, logout, fetchGallery, API_URL } from './api.js';
import { mockData } from './mock.js';
import { getGalleryHtml, initGalleryScreen } from './gallery-spa.js';

const mainContent = document.getElementById('enlarged-widget');

// Dynamic keyframe injection for smooth transitions
const style = document.createElement('style');
style.textContent = `
    @keyframes slideIn {
        from { opacity: 0; transform: translateY(15px); }
        to { opacity: 1; transform: translateY(0); }
    }
`;
document.head.appendChild(style);

export const ui = {
    renderScreen(html) {
        mainContent.innerHTML = html;
        // Trigger reflow to restart animation
        mainContent.style.animation = 'none';
        mainContent.offsetHeight; 
        mainContent.style.animation = 'slideIn 0.4s cubic-bezier(0.2, 0.8, 0.2, 1)';
    },

    showLogin() {
        const html = `
            <h2 class="screen-title">Přihlášení</h2>
            <div style="text-align: center; padding: 30px 10px;">
                <div style="font-size: 5rem; margin-bottom: 20px;">🔐</div>
                <h3 style="color: #333; margin-bottom: 15px;">Vítejte v Houbám Zdar!</h3>
                <p style="color: #666; margin-bottom: 30px; line-height: 1.5;">
                    Přihlášení a správa identity běží bezpečně přes systém <strong>AHOJ420</strong>.
                </p>
                <button class="btn-primary" id="real-login-btn" style="background-color: var(--secondary-color);">
                    Přihlásit se
                </button>
            </div>
        `;
        this.renderScreen(html);

        document.getElementById('real-login-btn').addEventListener('click', () => {
            login();
        });
    },

    showProfile() {
        if (!state.isLoggedIn) {
            this.showLogin();
            return;
        }

        const username = state.profile?.preferred_username || state.user?.preferred_username || "Neznámý houbař";
        const findsCount = state.profile?.public_posts_count || 0;
        const capturesCount = state.profile?.public_captures_count || 0;
        
        const html = `
            <h2 class="screen-title">Můj profil</h2>
            <div class="profile-card">
                <div class="avatar">👨‍🌾</div>
                <h3>${username}</h3>
                <p style="color: #666; margin-bottom: 20px;">Ověřený uživatel</p>
                
                <div class="profile-stats">
                    <div class="stat-item">
                        <span class="stat-value">${findsCount}</span>
                        <span class="stat-label">Příspěvků</span>
                    </div>
                    <div class="stat-item">
                        <span class="stat-value">${capturesCount}</span>
                        <span class="stat-label">Fotografií</span>
                    </div>
                </div>

                <button class="btn-primary btn-danger" id="real-logout-btn" style="margin-top: 30px;">Odhlásit se</button>
            </div>
        `;
        this.renderScreen(html);

        document.getElementById('real-logout-btn').addEventListener('click', async () => {
            await logout();
            this.showLogin();
        });
    },

    showGallery() {
        this.renderScreen(getGalleryHtml());
        initGalleryScreen();
    },

    showFeed() {
        let feedHtml = '<h2 class="screen-title">Zeď úlovků</h2>';
        if (mockData.feed.length === 0) {
            feedHtml += '<p style="text-align:center; color:#888;">Zatím zde nejsou žádné úlovky.</p>';
        } else {
            mockData.feed.forEach(item => {
                feedHtml += `
                    <div class="feed-item">
                        <img src="${item.img}" alt="${item.title}">
                        <div class="feed-content">
                            <h3>${item.title}</h3>
                            <p>📍 ${item.location}</p>
                            <small>👤 ${item.author} • 🕒 ${item.date}</small>
                        </div>
                    </div>
                `;
            });
        }
        this.renderScreen(feedHtml);
    },

    showMap() {
        const html = `
            <h2 class="screen-title">Mapa houbařů</h2>
            <div class="map-placeholder">
                <div>
                    <div style="font-size: 3rem; margin-bottom: 10px;">🗺️</div>
                    <div>Interaktivní mapa</div>
                    <div style="font-size: 0.9rem; color: #777; margin-top: 5px;">(Integrace Leaflet)</div>
                </div>
            </div>
            <button class="btn-primary" style="background-color: var(--primary-color);">Aktualizovat polohu</button>
        `;
        this.renderScreen(html);
    },

    showCamera() {
        if (!state.isLoggedIn) {
            this.showLogin();
            return;
        }

        const html = `
            <h2 class="screen-title">Přidat úlovek</h2>
            <div class="map-placeholder" style="background: #111; color: #fff; height: 400px; position: relative;">
                <div style="text-align: center;">
                    <div style="font-size: 4rem; margin-bottom: 15px;">📷</div>
                    <div style="color: #aaa;">Hledáček fotoaparátu</div>
                </div>
                <div style="position: absolute; border: 2px solid rgba(255,255,255,0.3); width: 80%; height: 80%; border-radius: 20px;"></div>
            </div>
            <button class="btn-primary">📸 Vyfotit</button>
        `;
        this.renderScreen(html);
    },
    
    showDefault(id, title, color) {
        const html = `
            <h2 class="screen-title" style="color: ${color}">${title}</h2>
            <div style="text-align: center; padding: 60px 20px;">
                <div style="font-size: 5rem; color: ${color}; opacity: 0.8; margin-bottom: 20px;">🛠️</div>
                <h3 style="color: #444;">Sekce ${id}</h3>
                <p style="color: #777; line-height: 1.6;">Tato část je ve vývoji.<br>Funkce budou přidány později.</p>
            </div>
        `;
        this.renderScreen(html);
    }
};