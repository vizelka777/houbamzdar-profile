(function bootstrapPwa() {
    if (typeof window === "undefined" || !window.isSecureContext) {
        return;
    }

    const BANNER_ID = "pwa-install-banner";
    const STYLE_ID = "pwa-install-banner-style";
    const DISMISS_UNTIL_KEY = "hzd_pwa_banner_dismiss_until_v1";
    const UPDATE_BADGE_KEY = "hzd_pwa_show_updated_v1";
    const DISMISS_DURATION_MS = 3 * 24 * 60 * 60 * 1000;
    const PROMPT_DELAY_MS = 1600;
    const IOS_GUIDE_DELAY_MS = 2400;
    const MANUAL_GUIDE_DELAY_MS = 4200;
    const UPDATE_BADGE_DURATION_MS = 4500;

    let deferredPrompt = null;
    let bannerTimer = null;
    let promptInProgress = false;
    let domReady = document.readyState !== "loading";
    let reloadingForUpdate = false;

    init();

    function init() {
        if ("serviceWorker" in navigator) {
            const hadControllerAtBoot = Boolean(navigator.serviceWorker.controller);

            navigator.serviceWorker.addEventListener("controllerchange", () => {
                if (!hadControllerAtBoot || reloadingForUpdate) {
                    return;
                }
                markUpdatedBadgeForNextLoad();
                reloadingForUpdate = true;
                window.location.reload();
            });

            window.addEventListener("load", () => {
                navigator.serviceWorker.register("/sw.js", { scope: "/" })
                    .then((registration) => {
                        registration.update().catch(() => {});

                        document.addEventListener("visibilitychange", () => {
                            if (document.visibilityState === "visible") {
                                registration.update().catch(() => {});
                            }
                        });
                    })
                    .catch((error) => {
                        console.warn("PWA service worker registration failed", error);
                    });
            });
        }

        if (!domReady) {
            document.addEventListener("DOMContentLoaded", handleDomReady, { once: true });
        } else {
            handleDomReady();
        }

        window.addEventListener("beforeinstallprompt", (event) => {
            event.preventDefault();
            deferredPrompt = event;
            if (shouldOfferInstallPrompt()) {
                scheduleBanner("prompt", PROMPT_DELAY_MS);
            }
        });

        window.addEventListener("appinstalled", () => {
            deferredPrompt = null;
            clearDismissedUntil();
            removeBanner();
        });

        const standaloneQuery = window.matchMedia ? window.matchMedia("(display-mode: standalone)") : null;
        if (standaloneQuery && typeof standaloneQuery.addEventListener === "function") {
            standaloneQuery.addEventListener("change", () => {
                if (isStandaloneMode()) {
                    removeBanner();
                }
            });
        }
    }

    function handleDomReady() {
        domReady = true;
        injectBannerStyles();
        maybeShowUpdatedBadge();

        if (shouldOfferIOSGuide()) {
            scheduleBanner("ios", IOS_GUIDE_DELAY_MS);
            return;
        }

        if (shouldOfferInstallPrompt()) {
            scheduleBanner("prompt", PROMPT_DELAY_MS);
            return;
        }

        if (shouldOfferManualGuide()) {
            scheduleBanner("manual", MANUAL_GUIDE_DELAY_MS);
        }
    }

    function scheduleBanner(kind, delayMs) {
        if (!domReady || isStandaloneMode() || isDismissed()) {
            return;
        }

        const existing = document.getElementById(BANNER_ID);
        if (existing && existing.dataset.kind === kind) {
            return;
        }

        window.clearTimeout(bannerTimer);
        bannerTimer = window.setTimeout(() => {
            if (kind === "prompt" && !shouldOfferInstallPrompt()) {
                return;
            }
            if (kind === "ios" && !shouldOfferIOSGuide()) {
                return;
            }
            if (kind === "manual" && !shouldOfferManualGuide()) {
                return;
            }
            renderBanner(kind);
        }, delayMs);
    }

    function renderBanner(kind) {
        removeBanner();

        const banner = document.createElement("section");
        banner.id = BANNER_ID;
        banner.className = "pwa-install-banner";
        banner.dataset.kind = kind;
        banner.setAttribute("role", "dialog");
        banner.setAttribute("aria-live", "polite");
        banner.setAttribute("aria-label", "Instalace aplikace");

        const content = kind === "prompt"
            ? buildPromptMarkup()
            : kind === "ios"
                ? buildIOSMarkup()
                : buildManualMarkup();
        banner.innerHTML = `
            <button type="button" class="pwa-install-close" aria-label="Zavřít nabídku instalace">&times;</button>
            <div class="pwa-install-brand">
                <img src="/icon-app-192.png" alt="" class="pwa-install-logo">
            </div>
            <div class="pwa-install-copy">
                ${content}
            </div>
        `;

        document.body.appendChild(banner);

        const closeButton = banner.querySelector(".pwa-install-close");
        closeButton.addEventListener("click", () => dismissBanner());

        if (kind === "prompt") {
            const installButton = banner.querySelector("[data-pwa-install]");
            const laterButton = banner.querySelector("[data-pwa-later]");
            installButton.addEventListener("click", () => promptInstall());
            laterButton.addEventListener("click", () => dismissBanner());
            return;
        }

        const okButton = banner.querySelector("[data-pwa-guide-ok]");
        okButton.addEventListener("click", () => dismissBanner());
    }

    function buildPromptMarkup() {
        return `
            <p class="pwa-install-eyebrow">Instalace aplikace</p>
            <h2>Otevřít Houbám Zdar jako aplikaci?</h2>
            <p>Spuštění bude rychlejší, aplikace dostane vlastní ikonu a základní rozhraní zůstane dostupné i při slabším připojení.</p>
            <div class="pwa-install-actions">
                <button type="button" class="pwa-install-primary" data-pwa-install>Instalovat</button>
                <button type="button" class="pwa-install-secondary" data-pwa-later>Později</button>
            </div>
        `;
    }

    function buildIOSMarkup() {
        return `
            <p class="pwa-install-eyebrow">iPhone</p>
            <h2>Na iPhonu se okno instalace neukazuje samo</h2>
            <p>Otevřete menu Sdílet v prohlížeči a vyberte <strong>Na plochu</strong>. Tím se Houbám Zdar nainstaluje jako webová aplikace.</p>
            <div class="pwa-install-actions">
                <button type="button" class="pwa-install-primary" data-pwa-guide-ok>Rozumím</button>
            </div>
        `;
    }

    function buildManualMarkup() {
        const mobile = isMobileViewport();
        const title = mobile
            ? "Když browser neukáže okno, instalace je pořád v menu"
            : "Když browser neukáže okno, instalace bývá v adresní liště";
        const description = mobile
            ? "Otevřete menu prohlížeče a hledejte <strong>Install app</strong> nebo <strong>Přidat na plochu</strong>."
            : "Podívejte se na ikonu instalace v adresní řádce nebo do menu prohlížeče na položku <strong>Install app</strong>.";

        return `
            <p class="pwa-install-eyebrow">Instalace</p>
            <h2>${title}</h2>
            <p>${description}</p>
            <div class="pwa-install-actions">
                <button type="button" class="pwa-install-primary" data-pwa-guide-ok>Rozumím</button>
            </div>
        `;
    }

    async function promptInstall() {
        if (!deferredPrompt || promptInProgress) {
            return;
        }

        promptInProgress = true;

        try {
            deferredPrompt.prompt();
            const choice = await deferredPrompt.userChoice;
            deferredPrompt = null;

            if (choice && choice.outcome === "accepted") {
                clearDismissedUntil();
                removeBanner();
            } else {
                dismissBanner();
            }
        } catch (error) {
            console.warn("PWA install prompt failed", error);
            dismissBanner();
        } finally {
            promptInProgress = false;
        }
    }

    function dismissBanner() {
        setDismissedUntil(Date.now() + DISMISS_DURATION_MS);
        removeBanner();
    }

    function removeBanner() {
        window.clearTimeout(bannerTimer);
        const existing = document.getElementById(BANNER_ID);
        if (existing) {
            existing.remove();
        }
    }

    function injectBannerStyles() {
        if (document.getElementById(STYLE_ID)) {
            return;
        }

        const style = document.createElement("style");
        style.id = STYLE_ID;
        style.textContent = `
            .pwa-update-badge {
                position: fixed;
                top: max(0.9rem, env(safe-area-inset-top, 0px) + 0.4rem);
                right: 1rem;
                z-index: 5001;
                display: inline-flex;
                align-items: center;
                gap: 0.55rem;
                max-width: min(calc(100vw - 1rem), 24rem);
                padding: 0.82rem 1rem;
                border-radius: 999px;
                border: 1px solid rgba(53, 93, 69, 0.14);
                background: rgba(255, 250, 242, 0.95);
                box-shadow: 0 18px 40px rgba(21, 34, 24, 0.16);
                backdrop-filter: blur(14px);
                color: #1b3729;
                font-size: 0.93rem;
                line-height: 1.2;
                animation: pwa-update-badge-in 220ms ease-out;
            }

            .pwa-update-badge::before {
                content: "";
                width: 0.7rem;
                height: 0.7rem;
                border-radius: 999px;
                background: #355d45;
                box-shadow: 0 0 0 0.28rem rgba(53, 93, 69, 0.12);
                flex: 0 0 auto;
            }

            @keyframes pwa-update-badge-in {
                from {
                    opacity: 0;
                    transform: translateY(-0.45rem);
                }
                to {
                    opacity: 1;
                    transform: translateY(0);
                }
            }

            .pwa-install-banner {
                position: fixed;
                left: 50%;
                bottom: max(1rem, env(safe-area-inset-bottom, 0px) + 0.5rem);
                transform: translateX(-50%);
                z-index: 5000;
                width: min(calc(100vw - 1rem), 33rem);
                display: grid;
                grid-template-columns: auto minmax(0, 1fr);
                gap: 0.95rem;
                padding: 1rem 1rem 1rem 0.95rem;
                border-radius: 1.45rem;
                border: 1px solid rgba(19, 38, 29, 0.12);
                background: linear-gradient(145deg, rgba(255, 250, 242, 0.98), rgba(244, 236, 223, 0.94));
                box-shadow: 0 22px 50px rgba(21, 34, 24, 0.22);
                backdrop-filter: blur(16px);
                color: #1b221c;
            }

            .pwa-install-brand {
                display: flex;
                align-items: flex-start;
                justify-content: center;
            }

            .pwa-install-logo {
                width: 3.4rem;
                height: 3.4rem;
                border-radius: 1rem;
                box-shadow: 0 12px 24px rgba(21, 34, 24, 0.14);
            }

            .pwa-install-copy {
                min-width: 0;
            }

            .pwa-install-eyebrow {
                margin: 0 0 0.3rem;
                color: #355d45;
                font-size: 0.76rem;
                font-weight: 700;
                letter-spacing: 0.16em;
                text-transform: uppercase;
            }

            .pwa-install-copy h2 {
                margin: 0 0 0.45rem;
                font-size: 1.15rem;
                line-height: 1.15;
                color: #13261d;
            }

            .pwa-install-copy p {
                margin: 0;
                font-size: 0.95rem;
                line-height: 1.45;
                color: #566158;
            }

            .pwa-install-copy strong {
                color: #1b3729;
            }

            .pwa-install-actions {
                display: flex;
                flex-wrap: wrap;
                gap: 0.55rem;
                margin-top: 0.85rem;
            }

            .pwa-install-primary,
            .pwa-install-secondary,
            .pwa-install-close {
                appearance: none;
                border: 0;
                font: inherit;
                cursor: pointer;
            }

            .pwa-install-primary,
            .pwa-install-secondary {
                min-height: 2.7rem;
                padding: 0.72rem 1rem;
                border-radius: 999px;
                font-weight: 700;
            }

            .pwa-install-primary {
                background: #355d45;
                color: #fff;
            }

            .pwa-install-secondary {
                background: rgba(53, 93, 69, 0.1);
                color: #1b3729;
            }

            .pwa-install-close {
                position: absolute;
                top: 0.55rem;
                right: 0.55rem;
                width: 2rem;
                height: 2rem;
                border-radius: 999px;
                background: rgba(19, 38, 29, 0.06);
                color: #355d45;
                font-size: 1.2rem;
                line-height: 1;
            }

            @media (max-width: 719px) {
                .pwa-update-badge {
                    top: max(0.45rem, env(safe-area-inset-top, 0px));
                    right: 0.3rem;
                    left: 0.3rem;
                    max-width: none;
                    border-radius: 1rem;
                    padding: 0.76rem 0.88rem;
                }

                .pwa-install-banner {
                    width: calc(100vw - 0.6rem);
                    bottom: max(0.3rem, env(safe-area-inset-bottom, 0px));
                    padding: 0.9rem 0.85rem 0.85rem 0.85rem;
                    border-radius: 1.15rem;
                    grid-template-columns: 1fr;
                    gap: 0.75rem;
                }

                .pwa-install-brand {
                    justify-content: flex-start;
                }

                .pwa-install-logo {
                    width: 3rem;
                    height: 3rem;
                    border-radius: 0.9rem;
                }

                .pwa-install-copy h2 {
                    padding-right: 1.8rem;
                    font-size: 1.03rem;
                }

                .pwa-install-copy p {
                    font-size: 0.92rem;
                }

                .pwa-install-actions {
                    display: grid;
                    grid-template-columns: repeat(2, minmax(0, 1fr));
                }

                .pwa-install-banner[data-kind="ios"] .pwa-install-actions,
                .pwa-install-banner[data-kind="manual"] .pwa-install-actions {
                    grid-template-columns: minmax(0, 1fr);
                }
            }
        `;

        document.head.appendChild(style);
    }

    function shouldOfferInstallPrompt() {
        return Boolean(deferredPrompt) && !isStandaloneMode() && !isDismissed();
    }

    function maybeShowUpdatedBadge() {
        if (!consumeUpdatedBadgeMarker()) {
            return;
        }

        const badge = document.createElement("div");
        badge.className = "pwa-update-badge";
        badge.setAttribute("role", "status");
        badge.setAttribute("aria-live", "polite");
        badge.textContent = "Aplikace byla aktualizována.";
        document.body.appendChild(badge);

        window.setTimeout(() => {
            badge.remove();
        }, UPDATE_BADGE_DURATION_MS);
    }

    function shouldOfferIOSGuide() {
        return isIOSDevice() && !isStandaloneMode() && !isDismissed() && !deferredPrompt;
    }

    function shouldOfferManualGuide() {
        return !isIOSDevice() && !isStandaloneMode() && !isDismissed() && !deferredPrompt;
    }

    function isStandaloneMode() {
        return (window.matchMedia && window.matchMedia("(display-mode: standalone)").matches) || window.navigator.standalone === true;
    }

    function isIOSDevice() {
        const userAgent = window.navigator.userAgent || "";
        return /iPad|iPhone|iPod/.test(userAgent) || (window.navigator.platform === "MacIntel" && window.navigator.maxTouchPoints > 1);
    }

    function isMobileViewport() {
        return window.matchMedia ? window.matchMedia("(max-width: 719px)").matches : window.innerWidth <= 719;
    }

    function isDismissed() {
        return getDismissedUntil() > Date.now();
    }

    function markUpdatedBadgeForNextLoad() {
        try {
            window.sessionStorage.setItem(UPDATE_BADGE_KEY, "1");
        } catch (error) {
            console.warn("Failed to persist PWA updated badge marker", error);
        }
    }

    function consumeUpdatedBadgeMarker() {
        try {
            const value = window.sessionStorage.getItem(UPDATE_BADGE_KEY);
            if (!value) {
                return false;
            }
            window.sessionStorage.removeItem(UPDATE_BADGE_KEY);
            return true;
        } catch (error) {
            return false;
        }
    }

    function getDismissedUntil() {
        try {
            return Number(window.localStorage.getItem(DISMISS_UNTIL_KEY) || 0);
        } catch (error) {
            return 0;
        }
    }

    function setDismissedUntil(value) {
        try {
            window.localStorage.setItem(DISMISS_UNTIL_KEY, String(value));
        } catch (error) {
            console.warn("Failed to persist PWA banner dismissal", error);
        }
    }

    function clearDismissedUntil() {
        try {
            window.localStorage.removeItem(DISMISS_UNTIL_KEY);
        } catch (error) {
            console.warn("Failed to clear PWA banner dismissal", error);
        }
    }
})();
