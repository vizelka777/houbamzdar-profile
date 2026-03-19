import { ui } from './ui.js';
import { state } from './api.js';

const widgetsContainer = document.getElementById('widgets-container');
const sideMenu = document.getElementById('side-menu');

const colors = [
    '#2A402A', // 1: Profile (Primary Dark Green)
    '#4CAF50', // 2: Feed (Secondary Green)
    '#556B2F', // 3: Map (Olive Drab)
    '#8B4513', // 4: Camera (Saddle Brown)
    '#CD853F', // 5: Gallery (Peru)
    '#6B8E23', '#465945', '#A0522D', '#BC8F8F', '#D2B48C'
];

const widgetConfig = [
    { id: 1, title: 'Profil', icon: '👤', action: () => ui.showProfile() },
    { id: 2, title: 'Zeď úlovků', icon: '📰', action: () => ui.showFeed() },
    { id: 3, title: 'Mapa', icon: '🗺️', action: () => ui.showMap() },
    { id: 4, title: 'Kamera', icon: '📷', action: () => ui.showCamera() },
    { id: 5, title: 'Galerie', icon: '🖼️', action: () => ui.showGallery() },
    { id: 6, title: 'Pravidla', icon: '📜', action: null },
    { id: 7, title: 'Žebříček', icon: '🏆', action: null },
    { id: 8, title: 'Nastavení', icon: '⚙️', action: null },
];

function handleWidgetClick(id) {
    sideMenu.classList.remove('open');
    
    // Smooth scroll to top of main content
    window.scrollTo({ top: 0, behavior: 'smooth' });

    const config = widgetConfig.find(w => w.id === id);
    if (config && config.action) {
        config.action();
    } else {
        const color = colors[(id - 1) % colors.length];
        const title = config ? config.title : `Funkce ${id}`;
        ui.showDefault(id, title, color);
    }
}

export function initWidgets(resetScreen = true) {
    widgetsContainer.innerHTML = '';
    
    // Create 8 widgets
    for (let i = 1; i <= 8; i++) {
        const widget = document.createElement('div');
        widget.className = 'mini-widget';
        widget.dataset.id = i;
        
        const config = widgetConfig.find(w => w.id === i);
        let icon = config ? config.icon : '✨';
        let title = config ? config.title : `Widget ${i}`;
        let bgColor = colors[(i - 1) % colors.length];
        
        if (i === 1) {
            icon = state.isLoggedIn ? '👨‍🌾' : '🔐';
            title = state.isLoggedIn ? 'Můj profil' : 'Přihlásit se';
        }

        widget.style.backgroundColor = bgColor;
        widget.innerHTML = `
            <div class="icon">${icon}</div>
            <div class="title">${title}</div>
        `;
        
        widget.addEventListener('click', () => handleWidgetClick(i));
        widgetsContainer.appendChild(widget);
    }
    
    // Initialize default screen (e.g., Feed)
    if (resetScreen) {
        ui.showFeed();
    }
}