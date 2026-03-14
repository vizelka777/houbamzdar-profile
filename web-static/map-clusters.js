(function attachMapClusterHelpers(global) {
    if (!global || typeof L === "undefined") {
        return;
    }

    function clusterTone(count) {
        if (count >= 24) {
            return "is-large";
        }
        if (count >= 8) {
            return "is-medium";
        }
        return "is-small";
    }

    function createClusterIcon(cluster) {
        const count = cluster.getChildCount();
        return L.divIcon({
            className: "hzd-cluster-icon",
            html: `
                <span class="hzd-cluster-bubble ${clusterTone(count)}">
                    <strong>${count}</strong>
                </span>
            `,
            iconSize: [58, 58]
        });
    }

    function hasClusterSupport() {
        return typeof L.markerClusterGroup === "function";
    }

    function createLayer(markers, options = {}) {
        const list = Array.isArray(markers) ? markers : [];
        const shouldCluster = list.length > 1 && hasClusterSupport() && options.useCluster !== false;

        if (shouldCluster) {
            const layer = L.markerClusterGroup({
                animate: true,
                animateAddingMarkers: true,
                chunkedLoading: true,
                showCoverageOnHover: false,
                spiderfyOnMaxZoom: true,
                zoomToBoundsOnClick: true,
                maxClusterRadius: 54,
                spiderfyDistanceMultiplier: 1.18,
                spiderLegPolylineOptions: {
                    weight: 1.5,
                    color: "#2d5d41",
                    opacity: 0.48
                },
                iconCreateFunction: createClusterIcon,
                ...(options.clusterOptions || {})
            });

            list.forEach((marker) => layer.addLayer(marker));
            return layer;
        }

        const layer = L.featureGroup();
        list.forEach((marker) => layer.addLayer(marker));
        return layer;
    }

    function replaceLayer(map, currentLayer, markers, options = {}) {
        if (!map) {
            return null;
        }

        if (currentLayer && map.hasLayer(currentLayer)) {
            map.removeLayer(currentLayer);
        }
        if (currentLayer && typeof currentLayer.clearLayers === "function") {
            currentLayer.clearLayers();
        }

        const nextLayer = createLayer(markers, options);
        if (nextLayer) {
            nextLayer.addTo(map);
        }
        return nextLayer;
    }

    function fitLayer(map, layer, options = {}) {
        if (!map || !layer || typeof layer.getBounds !== "function") {
            return;
        }

        const bounds = layer.getBounds();
        if (!bounds || !bounds.isValid()) {
            return;
        }

        const fitOptions = {
            padding: options.padding || [30, 30]
        };

        if (typeof options.maxZoom === "number") {
            fitOptions.maxZoom = options.maxZoom;
        }

        map.fitBounds(bounds, fitOptions);
    }

    global.HZDMapClusters = {
        hasClusterSupport,
        createClusterIcon,
        replaceLayer,
        fitLayer
    };
})(window);
