import { initWidgets } from './widgets.js';
import { ui } from './ui.js';
import { checkAuth, login, logout, state } from './api.js';

function updateHeaderAuthIcon() {
    const btn = document.getElementById('header-profile-btn');
    if (state.isLoggedIn) {
        btn.title = "Odhlásit se";
        btn.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" class="header-svg-icon"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"></path><polyline points="16 17 21 12 16 7"></polyline><line x1="21" y1="12" x2="9" y2="12"></line></svg>`;
    } else {
        btn.title = "Přihlásit se";
        btn.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" class="header-svg-icon"><path d="M15 3h4a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2h-4"></path><polyline points="10 17 15 12 10 7"></polyline><line x1="15" y1="12" x2="3" y2="12"></line></svg>`;
    }
}

document.addEventListener('DOMContentLoaded', async () => {
    const sideMenu = document.getElementById('side-menu');
    const menuToggleBtn = document.getElementById('menu-toggle-btn');
    const headerCameraBtn = document.getElementById('header-camera-btn');
    const headerProfileBtn = document.getElementById('header-profile-btn');

    // Fetch initial auth state
    await checkAuth();

    // Header buttons
    headerCameraBtn.addEventListener('click', () => {
        sideMenu.classList.remove('open');
        window.scrollTo({ top: 0, behavior: 'smooth' });
        ui.showCamera();
    });

    headerProfileBtn.addEventListener('click', () => {
        sideMenu.classList.remove('open');
        if (state.isLoggedIn) {
            logout();
        } else {
            login();
        }
    });

    // Toggle Menu
    menuToggleBtn.addEventListener('click', (e) => {
        e.stopPropagation(); // Prevent immediate closing
        sideMenu.classList.toggle('open');
    });

    // Close menu when clicking outside
    document.addEventListener('click', (e) => {
        if (!sideMenu.contains(e.target) && !menuToggleBtn.contains(e.target)) {
            if (sideMenu.classList.contains('open')) {
                sideMenu.classList.remove('open');
            }
        }
    });

    // Prevent closing when clicking inside the menu
    sideMenu.addEventListener('click', (e) => {
        e.stopPropagation();
    });

    // Initialize the widget system
    initWidgets();
    updateHeaderAuthIcon();

    // Re-render widgets when auth state changes
    document.addEventListener('auth-changed', () => {
        initWidgets(false); // pass false to avoid resetting the active screen
        updateHeaderAuthIcon();
    });
});