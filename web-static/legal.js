(function () {
    const footerSignature = "Ing. Stanislav Vizelka @ vibe coding 2026";
    const params = new URLSearchParams(window.location.search);
    if (params.get("from") === "register") {
        const topLink = document.getElementById("legal-top-link");
        if (topLink) {
            topLink.textContent = "Zavřít";
            topLink.setAttribute("href", "/register.html");
            topLink.setAttribute("aria-label", "Zavřít");
            topLink.addEventListener("click", function (event) {
                event.preventDefault();
                if (window.history.length > 1) {
                    window.history.back();
                } else {
                    window.close();
                }
                window.setTimeout(function () {
                    window.location.href = "/register.html";
                }, 150);
            });
        }

        document
            .querySelectorAll("a[href^='/terms.html'], a[href^='/privacy.html'], a[href^='/cookies.html'], a[href^='/rules.html']")
            .forEach(function (link) {
                const url = new URL(link.getAttribute("href"), window.location.origin);
                url.searchParams.set("from", "register");
                link.setAttribute("href", url.pathname + url.search + url.hash);
            });
    }

    const legalCard = document.querySelector(".legal-card");
    if (!legalCard) return;

    let footer = legalCard.querySelector(".legal-footer");
    if (!footer) {
        footer = document.createElement("footer");
        footer.className = "legal-footer";
        legalCard.appendChild(footer);
    }

    if (!footer.querySelector(".legal-credit")) {
        const credit = document.createElement("p");
        credit.className = "legal-credit";
        credit.textContent = footerSignature;
        footer.appendChild(credit);
    }
})();
