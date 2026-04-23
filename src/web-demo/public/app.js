const providerList = document.querySelector("#providerList");
const providerSelect = document.querySelector("#providerSelect");
const modelSelect = document.querySelector("#modelSelect");
const capabilitySelect = document.querySelector("#capabilitySelect");
const aspectRatioSelect = document.querySelector("#aspectRatioSelect");
const resolutionSelect = document.querySelector("#resolutionSelect");
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

function activeModel() {
  return activeProvider()?.models.find((model) => model.id === modelSelect.value);
}

function renderProviders() {
  providerList.innerHTML = providers
    .map(
      (provider) => `
        <button class="provider-item" type="button" data-provider="${provider.providerId}">
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
      renderModels();
    });
  });

  renderModels();
}

function renderModels() {
  const provider = activeProvider();
  modelSelect.innerHTML = (provider?.models ?? [])
    .map((model) => `<option value="${model.id}">${model.label ?? model.id}</option>`)
    .join("");
  renderCapabilities();
}

function renderCapabilities() {
  const model = activeModel();
  const caps = model?.capabilities ?? [];
  capabilitySelect.innerHTML = caps
    .map((capability) => `<option value="${capability}">${capability}</option>`)
    .join("");
}

async function loadProviders() {
  statusText.textContent = "Loading providers...";
  const res = await fetch("/api/providers");
  const data = await res.json();
  providers = data.providers;
  renderProviders();
  resultBox.textContent = JSON.stringify(data, null, 2);
  imageGrid.innerHTML = `<p class="empty">Images returned by image providers will appear here.</p>`;
  statusText.textContent = `${providers.length} providers available`;
}

async function runGenerate(event) {
  event.preventDefault();
  const provider = activeProvider();
  const providerRef = `${provider.providerId}/${modelSelect.value}`;

  runBtn.disabled = true;
  statusText.textContent = `Running ${providerRef}...`;
  imageGrid.innerHTML = `<p class="empty">Waiting for result...</p>`;

  try {
    const res = await fetch("/api/generate", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        providerRef,
        capability: capabilitySelect.value,
        prompt: promptInput.value,
        aspectRatio: aspectRatioSelect.value,
        resolution: resolutionSelect.value,
        count: 1,
      }),
    });
    const data = await res.json();
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

providerSelect.addEventListener("change", renderModels);
modelSelect.addEventListener("change", renderCapabilities);
refreshBtn.addEventListener("click", loadProviders);
generateForm.addEventListener("submit", runGenerate);

loadProviders();
