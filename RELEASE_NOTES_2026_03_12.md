# Přehled provedených prací – 12. března 2026

Dnes byla provedena rozsáhlá aktualizace projektu Houbam Zdar, která vyřešila kritické problémy s nahráváním fotografií a přidala klíčové sociální funkce.

## 1. Opravy nahrávání a zpracování fotografií
- **Podpora iPhone (HEIC)**: Implementována knihovna `heic2any` na frontendu. iPhony nyní automaticky převádějí fotky do JPEG před odesláním, což šetří data a zajišťuje kompatibilitu.
- **Oprava rotace (EXIF)**: Vyřešen problém s "otočenými" fotkami. Veškeré snímky jsou nyní na klientovi přeresleny přes HTML5 Canvas, čímž se fyzicky srovnají a odstraní se problematická metadata.
- **Optimalizace velikosti**: Fotografie jsou před nahráním zmenšeny na max. 1920px, což výrazně zrychluje upload v místech se slabým signálem (v lese).
- **Rozšíření formátů**: Backend nyní podporuje a normalizuje formáty WebP a GIF.

## 2. Nový systém publikací (Příspěvky)
- **Editor publikací**: Vytvořena stránka pro tvorbu příspěvků s textem a výběrem až 9 fotografií z archivu.
- **Správa obsahu**: Přidána možnost **upravovat** a **mazat** vlastní publikace přímo z veřejného profilu.
- **Stránkování**: Výběr fotografií v editoru je nyní stránkovaný (po 10 kusech) pro lepší stabilitu na mobilních zařízeních.

## 3. Komunitní funkce
- **Veřejná zeď (Feed)**: Nová sekce "Zeď úlovků" zobrazující nejnovější příspěvky od všech uživatelů.
- **Veřejná galerie**: Samostatná stránka se všemi sdílenými fotografiemi v přehledné mřížce.
- **Lightbox (Galerie)**: Implementováno celoobrazovkové prohlížení fotek s možností listování (galerie) po kliknutí na jakýkoliv úlovek.
- **Lajky (Zástupka)**: Připraven vizuální prvek pro lajkování příspěvků (zatím jako grafická příprava pro budoucí logiku).

## 4. Infrastruktura a Deployment
- **CORS**: Povolena metoda `PUT` pro umožnění úprav příspěvků z webového prohlížeče.
- **Bunny.net Configuration**: Opraveny konfigurační manifesty `app_v9.json`, obnoveny chybějící proměnné prostředí pro storage.
- **Verzování**: Projekt byl povýšen přes několik verzí až na finální **v19**.
- **CDN**: Automatizováno promazávání cache při aktualizaci statických souborů.

---
*Všechny změny jsou nasazeny na produkci a uloženy v repozitáři ve větvi `prace`.*
