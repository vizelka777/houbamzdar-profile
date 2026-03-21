import * as BunnySDK from "@bunny.net/edgescript-sdk";
import process from "node:process";

const GEMINI_API_KEY = process.env.GEMINI_API_KEY || "";
const VALIDATOR_TOKEN = process.env.CAPTURE_AI_VALIDATOR_TOKEN || "";
const DEFAULT_PUBLISH_GEMINI_MODEL = stripModelPrefix(process.env.PUBLISH_GEMINI_MODEL);
const DEFAULT_MODERATOR_GEMINI_MODEL = stripModelPrefix(process.env.MODERATOR_DEFAULT_GEMINI_MODEL);
const BUNNY_STORAGE_HOST = process.env.BUNNY_STORAGE_HOST || "storage.bunnycdn.com";
const BUNNY_PRIVATE_STORAGE_ZONE = process.env.BUNNY_PRIVATE_STORAGE_ZONE || "houbamzdarprivateedge";
const BUNNY_PRIVATE_STORAGE_KEY = process.env.BUNNY_PRIVATE_STORAGE_KEY || "";
const REVIEW_MODE_PUBLISH_VALIDATION = "publish_validation";
const REVIEW_MODE_MODERATOR_RECHECK = "moderator_recheck";
const MODEL_CATALOG_TTL_MS = 10 * 60 * 1000;
const COMMON_HEADERS = {
    "content-type": "application/json; charset=utf-8",
    "cache-control": "no-store",
    "x-content-type-options": "nosniff"
};
let modelCatalogCache = {
    fetchedAt: 0,
    catalog: null
};

function requireConfiguredModel(value, envName) {
    const code = stripModelPrefix(value);
    if (!code) {
        throw new Error(`Missing required Bunny environment variable ${envName}`);
    }
    return code;
}

function currentDefaultPublishModel() {
    return requireConfiguredModel(DEFAULT_PUBLISH_GEMINI_MODEL, "PUBLISH_GEMINI_MODEL");
}

function currentDefaultModeratorModel() {
    return requireConfiguredModel(DEFAULT_MODERATOR_GEMINI_MODEL, "MODERATOR_DEFAULT_GEMINI_MODEL");
}

function buildPublicConfig() {
    return {
        ok: true,
        script: "houbamzdar-ai-analyze-api",
        publish_default_model: currentDefaultPublishModel(),
        moderator_default_model: currentDefaultModeratorModel()
    };
}

function json(data, status = 200) {
    return new Response(JSON.stringify(data), {
        status,
        headers: COMMON_HEADERS
    });
}

function arrayBufferToBase64(buffer) {
    const bytes = new Uint8Array(buffer);
    const chunkSize = 0x8000;
    let binary = "";

    for (let idx = 0; idx < bytes.length; idx += chunkSize) {
        const chunk = bytes.subarray(idx, idx + chunkSize);
        binary += String.fromCharCode(...chunk);
    }

    return btoa(binary);
}

async function readPrivateCapture(privateStorageKey) {
    if (!BUNNY_PRIVATE_STORAGE_ZONE || !BUNNY_PRIVATE_STORAGE_KEY) {
        throw new Error("Missing Bunny private storage configuration");
    }

    const response = await fetch(`https://${BUNNY_STORAGE_HOST}/${BUNNY_PRIVATE_STORAGE_ZONE}/${privateStorageKey}`, {
        headers: {
            AccessKey: BUNNY_PRIVATE_STORAGE_KEY
        }
    });

    if (!response.ok) {
        throw new Error(`Failed to fetch private capture: ${response.status}`);
    }

    return response;
}

async function readValidatorImage({ imageURL = "", privateStorageKey = "" } = {}) {
	const optimizerURL = String(imageURL || "").trim();
	if (optimizerURL) {
		const response = await fetch(optimizerURL);
		if (!response.ok) {
			throw new Error(`Failed to fetch optimizer image: ${response.status}`);
		}
		return response;
	}

	return readPrivateCapture(privateStorageKey);
}

function normalizeSpecies(items) {
	if (!Array.isArray(items)) {
		return [];
	}

	const deduped = new Map();

	items
		.filter(Boolean)
		.map((item) => ({
			latin_name: String(item.latin_name || "").trim(),
			czech_official_name: String(item.czech_official_name || "").trim(),
			probability: Math.max(0, Math.min(1, Number(item.probability) || 0))
		}))
		.filter((item) => item.latin_name && item.probability > 0)
		.forEach((item) => {
			const key = item.latin_name.toLowerCase();
			const previous = deduped.get(key);
			if (!previous || item.probability > previous.probability) {
				deduped.set(key, item);
			}
		});

	return Array.from(deduped.values()).sort((left, right) => right.probability - left.probability);
}

function stripModelPrefix(value) {
    return String(value || "").replace(/^models\//, "").trim();
}

function isSelectableModeratorModel(model) {
    const code = stripModelPrefix(model?.name);
    if (!code || !code.startsWith("gemini-")) {
        return false;
    }
    const methods = Array.isArray(model?.supportedGenerationMethods)
        ? model.supportedGenerationMethods
        : [];
    if (!methods.includes("generateContent")) {
        return false;
    }

    const excludedFragments = [
        "tts",
        "audio",
        "robotics",
        "computer-use",
        "deep-research",
        "embedding"
    ];
    return !excludedFragments.some((fragment) => code.includes(fragment));
}

function moderatorModelRank(code) {
    const ranking = [
        "gemini-3.1-pro-preview",
        "gemini-3-pro-preview",
        "gemini-2.5-pro",
        "gemini-pro-latest",
        "gemini-3-flash-preview",
        "gemini-3.1-flash-image-preview",
        "gemini-2.5-flash",
        "gemini-flash-latest",
        "gemini-2.5-flash-lite",
        "gemini-flash-lite-latest",
        "gemini-2.0-flash",
        "gemini-2.0-flash-001",
        "gemini-2.0-flash-lite",
        "gemini-2.0-flash-lite-001"
    ];
    const idx = ranking.indexOf(code);
    return idx === -1 ? ranking.length + 100 : idx;
}

async function fetchModeratorModelCatalog(forceRefresh = false) {
    const now = Date.now();
    if (!forceRefresh && modelCatalogCache.catalog && now-modelCatalogCache.fetchedAt < MODEL_CATALOG_TTL_MS) {
        return modelCatalogCache.catalog;
    }

    const response = await fetch(`https://generativelanguage.googleapis.com/v1beta/models?key=${encodeURIComponent(GEMINI_API_KEY)}`);
    const payload = await response.json();
    if (!response.ok) {
        throw new Error(payload?.error?.message || `Failed to list Gemini models: ${response.status}`);
    }

    const models = Array.isArray(payload?.models)
        ? payload.models
            .filter(isSelectableModeratorModel)
            .map((model) => ({
                code: stripModelPrefix(model.name),
                label: stripModelPrefix(model.name)
            }))
            .sort((left, right) => {
                const rankDiff = moderatorModelRank(left.code) - moderatorModelRank(right.code);
                if (rankDiff !== 0) {
                    return rankDiff;
                }
                return left.code.localeCompare(right.code);
            })
        : [];

    const configuredModeratorModel = currentDefaultModeratorModel();
    const defaultModel = models.find((item) => item.code === configuredModeratorModel) || null;
    if (!defaultModel) {
        throw new Error(`Configured moderator model "${configuredModeratorModel}" is not available in the current Gemini catalog`);
    }

    const catalog = {
        ok: true,
        default_model: defaultModel.code,
        publish_default_model: currentDefaultPublishModel(),
        models
    };
    modelCatalogCache = {
        fetchedAt: now,
        catalog
    };
    return catalog;
}

async function pickGeminiModel(reviewMode, requestedModel = "") {
    if (reviewMode !== REVIEW_MODE_MODERATOR_RECHECK) {
        return currentDefaultPublishModel();
    }

    const catalog = await fetchModeratorModelCatalog();
    const requestedCode = stripModelPrefix(requestedModel);
    if (requestedCode && catalog.models.some((item) => item.code === requestedCode)) {
        return requestedCode;
    }
    return catalog.default_model || currentDefaultModeratorModel();
}

function buildPrompt(reviewMode) {
	const advancedReview = reviewMode === REVIEW_MODE_MODERATOR_RECHECK;
	return [
		"Analyzuj fotografii hub.",
		"Nejprve rozhodni, zda je na snímku alespoň jedna houba nebo plodnice houby.",
		"Pokud ano, vrať všechny vizuálně rozlišitelné taxony hub, které dokážeš na snímku rozpoznat.",
		"Nevracej jen jeden druh, pokud je na snímku rozpoznatelných více druhů nebo taxonů.",
		"Latinské jméno je primární a musí být co nejpřesnější.",
		"Používej pouze kanonické vědecké latinské názvy, ne synonyma ani lidové názvy.",
		"Pokud nelze druh spolehlivě určit, vrať nejnižší obhajitelný taxon, například rod, místo vymyšleného druhu.",
		"czech_official_name musí odpovídat stejnému taxonu jako latin_name.",
		"Pokud si nejsi jistý oficiálním českým názvem, vrať pro czech_official_name prázdný řetězec.",
		"Každý taxon uveď pouze jednou a duplicity slouč.",
		"probability musí být číslo od 0 do 1 a vyjadřuje pouze tvou vizuální jistotu pro daný taxon.",
		advancedReview
			? "Jde o moderatorskou kontrolu, proto upřednostni taxonomickou přesnost a konzervativnost před agresivním určováním na úroveň druhu."
			: "Upřednostni konzistenci latinských názvů a nevymýšlej příliš specifický druh, pokud to obraz spolehlivě nepotvrzuje.",
		"Nevracej count, summary ani žádný další text.",
		"Odpověz striktně jako JSON v tomto tvaru:",
		"{\"has_mushrooms\":true,\"species\":[{\"latin_name\":\"Boletus edulis\",\"czech_official_name\":\"hřib smrkový\",\"probability\":0.97},{\"latin_name\":\"Amanita muscaria\",\"czech_official_name\":\"muchomůrka červená\",\"probability\":0.61}]}",
		"nebo {\"has_mushrooms\":false,\"species\":[]}."
	].join(" ");
}

async function analyzeCapture({ privateStorageKey = "", imageURL = "", inlineImageData = "", inlineImageMimeType = "", reviewMode = REVIEW_MODE_PUBLISH_VALIDATION, requestedModel = "" } = {}) {
	if (!GEMINI_API_KEY) {
		throw new Error("Missing GEMINI_API_KEY");
	}

	let mimeType = String(inlineImageMimeType || "").trim() || "image/jpeg";
	let inlineImage = String(inlineImageData || "").trim();
	if (!inlineImage) {
		const imageResponse = await readValidatorImage({ imageURL, privateStorageKey });
		const imageBuffer = await imageResponse.arrayBuffer();
		mimeType = imageResponse.headers.get("content-type") || "image/jpeg";
		inlineImage = arrayBufferToBase64(imageBuffer);
	}
	const prompt = buildPrompt(reviewMode);
	const modelCode = await pickGeminiModel(reviewMode, requestedModel);

	const geminiResponse = await fetch(
		`https://generativelanguage.googleapis.com/v1beta/models/${modelCode}:generateContent?key=${encodeURIComponent(GEMINI_API_KEY)}`,
		{
			method: "POST",
			headers: {
				"content-type": "application/json"
            },
            body: JSON.stringify({
                contents: [{
                    parts: [
                        { text: prompt },
                        { inline_data: { mime_type: mimeType, data: inlineImage } }
                    ]
				}],
				generationConfig: {
					response_mime_type: "application/json",
					temperature: 0.1
				}
			})
		}
	);

    const raw = await geminiResponse.json();
    if (!geminiResponse.ok) {
        throw new Error(raw?.error?.message || `Gemini status ${geminiResponse.status}`);
    }

    const jsonText = raw?.candidates?.[0]?.content?.parts?.[0]?.text;
    if (!jsonText) {
        throw new Error("Gemini returned no structured content");
    }

    const parsed = JSON.parse(jsonText);
    const species = normalizeSpecies(parsed.species);

	return {
		ok: true,
		has_mushrooms: Boolean(parsed.has_mushrooms) && species.length > 0,
		model_code: modelCode,
		species
	};
}

BunnySDK.net.http.serve(async (request) => {
    const url = new URL(request.url);

    if (url.pathname === "/health") {
        return json({ ok: true, script: "houbamzdar-ai-analyze-api" });
    }

    if (url.pathname === "/config") {
        try {
            return json(buildPublicConfig());
        } catch (error) {
            return json({ ok: false, error: error instanceof Error ? error.message : "invalid validator config" }, 500);
        }
    }

    if (url.pathname === "/models") {
        if (VALIDATOR_TOKEN) {
            const authHeader = request.headers.get("authorization") || "";
            if (authHeader !== `Bearer ${VALIDATOR_TOKEN}`) {
                return json({ error: "Unauthorized" }, 401);
            }
        }
        try {
            return json(await fetchModeratorModelCatalog());
        } catch (error) {
            return json({ error: error instanceof Error ? error.message : "failed to list models" }, 500);
        }
    }

    if (url.pathname !== "/validate-capture") {
        return json({ error: "Not found" }, 404);
    }

    if (request.method !== "POST") {
        return json({ error: "Method not allowed" }, 405);
    }

    if (VALIDATOR_TOKEN) {
        const authHeader = request.headers.get("authorization") || "";
        if (authHeader !== `Bearer ${VALIDATOR_TOKEN}`) {
            return json({ error: "Unauthorized" }, 401);
        }
    }

	try {
		const body = await request.json();
		const captureID = String(body?.capture_id || "").trim();
		const privateStorageKey = String(body?.private_storage_key || "").trim();
		const imageURL = String(body?.image_url || "").trim();
		const inlineImageData = String(body?.inline_image_data || "").trim();
		const inlineImageMimeType = String(body?.inline_image_mime_type || "").trim();
		const reviewMode = String(body?.review_mode || REVIEW_MODE_PUBLISH_VALIDATION).trim() || REVIEW_MODE_PUBLISH_VALIDATION;
        const requestedModel = String(body?.model_code || "").trim();

		if (!captureID || (!privateStorageKey && !imageURL && !inlineImageData)) {
			return json({ error: "Missing capture_id or image payload" }, 400);
		}

		const result = await analyzeCapture({
			privateStorageKey,
			imageURL,
			inlineImageData,
			inlineImageMimeType,
			reviewMode,
			requestedModel
		});
		return json(result);
	} catch (error) {
        return json({ error: error instanceof Error ? error.message : "validator failed" }, 500);
    }
});
