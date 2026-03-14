document.addEventListener("DOMContentLoaded", async () => {
    const mapContainer = document.getElementById("global-map");
    if (!mapContainer) return;

    // Initialize Leaflet map
    // Centered on Czech Republic by default
    const map = L.map('global-map').setView([49.8, 15.5], 7);

    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
        attribution: '&copy; <a href="https://www.openstreetmap.org/copyright">OpenStreetMap</a> contributors'
    }).addTo(map);

    try {
        const res = await apiGet("/api/public/captures?limit=500");
        if (res && res.ok && res.captures) {
            const captures = res.captures;
            
            // Create a bounds object to fit map to markers
            const bounds = L.latLngBounds();
            let hasValidPoints = false;

            captures.forEach(capture => {
                if (capture.latitude && capture.longitude) {
                    hasValidPoints = true;
                    const lat = parseFloat(capture.latitude);
                    const lon = parseFloat(capture.longitude);
                    
                    if (!isNaN(lat) && !isNaN(lon)) {
                        const marker = L.marker([lat, lon]).addTo(map);
                        bounds.extend([lat, lon]);

                        const imgUrl = capture.public_url ? escapeHtml(capture.public_url) : `${API_URL}/api/captures/${encodeURIComponent(capture.id)}/preview`;
                        const author = escapeHtml(capture.author_name || "Neznámý houbař");
                        const date = escapeHtml(formatDateTime(capture.captured_at));

                        const popupContent = `
                            <div class="map-popup-content">
                                <a href="${imgUrl}" target="_blank">
                                    <img src="${imgUrl}" alt="Nález" loading="lazy">
                                </a>
                                <h4>${author}</h4>
                                <p>${date}</p>
                            </div>
                        `;
                        marker.bindPopup(popupContent);
                    }
                }
            });

            if (hasValidPoints) {
                map.fitBounds(bounds, { padding: [30, 30] });
            }
        }
    } catch (err) {
        console.error("Failed to load captures for map", err);
    }
});