const providerList = document.querySelector("#providerList");
const providerSelect = document.querySelector("#providerSelect");
const promptInput = document.querySelector("#promptInput");
const resultBox = document.querySelector("#resultBox");
const imageGrid = document.querySelector("#imageGrid");
const statusText = document.querySelector("#statusText");
const refreshBtn = document.querySelector("#refreshBtn");
const generateForm = document.querySelector("#generateForm");
const runBtn = document.querySelector("#runBtn");

let providers = [];

function activeProvider() {
  return providers.find((provider) => provider.providerId === providerSelect.value);
}

function renderProviders() {
  if (!providers.length) {
    providerList.innerHTML = `<p class="empty">No providers available.</p>`;
    providerSelect.innerHTML = "";
    return;
  }

  if (!activeProvider()) {
    providerSelect.value = providers[0].providerId;
  }

  providerList.innerHTML = providers
    .map(
      (provider) => `
        <button class="provider-item" type="button" data-provider="${provider.providerId}" data-active="${provider.providerId === providerSelect.value}">
          <strong>${provider.label}</strong>
          <span>${provider.providerId} · ${provider.transportStrategy.primary}</span>
        </button>
      `,
    )
    .join("");

  providerSelect.innerHTML = providers
    .map((provider) => `<option value="${provider.providerId}">${provider.label}</option>`)
    .join("");

  providerList.querySelectorAll("button").forEach((button) => {
    button.addEventListener("click", () => {
      providerSelect.value = button.dataset.provider;
      renderProviderSelection();
    });
  });

  renderProviderSelection();
}

function renderProviderSelection() {
  providerList.querySelectorAll("button").forEach((button) => {
    button.dataset.active = String(button.dataset.provider === providerSelect.value);
  });
}

async function loadProviders() {
  statusText.textContent = "Loading providers...";
  try {
    const res = await fetch("/api/providers");
    const data = await readJsonResponse(res);
    if (!res.ok) {
      throw new Error(data.error ?? `Provider request failed with ${res.status}`);
    }
    if (!Array.isArray(data.providers)) {
      throw new Error("Provider response missing providers array");
    }
    providers = data.providers;
    renderProviders();
    resultBox.textContent = JSON.stringify(data, null, 2);
    imageGrid.innerHTML = `<p class="empty">Images returned by image providers will appear here.</p>`;
    statusText.textContent = `${providers.length} providers available`;
  } catch (error) {
    providers = [];
    renderProviders();
    const message = error instanceof Error ? error.message : String(error);
    resultBox.textContent = JSON.stringify({ error: message }, null, 2);
    statusText.textContent = "Failed to load providers";
  }
}

async function runGenerate(event) {
  event.preventDefault();
  const provider = activeProvider();
  if (!provider) {
    statusText.textContent = "No provider selected";
    return;
  }
  const providerRef = `${provider.providerId}/web`;

  runBtn.disabled = true;
  statusText.textContent = `Running ${providerRef}...`;
  imageGrid.innerHTML = `<p class="empty">Waiting for result...</p>`;

  try {
    const res = await fetch("/api/generate", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        providerRef,
        prompt: promptInput.value,
        count: 1,
      }),
    });
    const data = await readJsonResponse(res);
    resultBox.textContent = JSON.stringify(data, null, 2);
    const images = data.output?.images ?? [];
    imageGrid.innerHTML = "";
    const visibleImages = images.filter((image) => image.url);
    if (visibleImages.length) {
      for (const image of visibleImages) {
        const img = document.createElement("img");
        img.src = image.url;
        img.alt = "Generated image";
        imageGrid.appendChild(img);
      }
    } else {
      imageGrid.innerHTML = `<p class="empty">No images in this result.</p>`;
    }
    statusText.textContent = res.ok ? "Done" : "Request failed";
  } catch (error) {
    resultBox.textContent = JSON.stringify(
      { error: error instanceof Error ? error.message : String(error) },
      null,
      2,
    );
    statusText.textContent = "Request failed";
  } finally {
    runBtn.disabled = false;
  }
}

async function readJsonResponse(res) {
  const raw = await res.text();
  if (!raw.trim()) {
    throw new Error(`Empty response with status ${res.status}`);
  }
  try {
    return JSON.parse(raw);
  } catch (error) {
    throw new Error(
      `Invalid JSON response with status ${res.status}: ${error instanceof Error ? error.message : String(error)}`,
    );
  }
}

providerSelect.addEventListener("change", () => {
  renderProviderSelection();
});
refreshBtn.addEventListener("click", loadProviders);
generateForm.addEventListener("submit", runGenerate);

loadProviders();
