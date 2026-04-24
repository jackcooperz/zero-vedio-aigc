const providerList = document.querySelector("#providerList");
const providerSelect = document.querySelector("#providerSelect");
const promptInput = document.querySelector("#promptInput");
const imageInput = document.querySelector("#imageInput");
const resultBox = document.querySelector("#resultBox");
const imageGrid = document.querySelector("#imageGrid");
const statusText = document.querySelector("#statusText");
const refreshBtn = document.querySelector("#refreshBtn");
const generateForm = document.querySelector("#generateForm");
const runBtn = document.querySelector("#runBtn");
const selectedImagePreview = document.querySelector("#selectedImagePreview");

let providers = [];
let selectedImages = [];

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
    const inputImages = await Promise.all(selectedImages.map((item) => readFileAsPayload(item.file)));
    const res = await fetch("/api/generate", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        providerRef,
        prompt: promptInput.value,
        images: inputImages,
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

function addSelectedImages(files) {
  for (const file of files) {
    if (!file.type.startsWith("image/")) {
      continue;
    }
    selectedImages.push({
      id: `${file.name}:${file.size}:${file.lastModified}:${crypto.randomUUID?.() ?? Math.random()}`,
      file,
      previewUrl: URL.createObjectURL(file),
    });
  }
  imageInput.value = "";
  renderSelectedImages();
}

function removeSelectedImage(id) {
  const image = selectedImages.find((item) => item.id === id);
  if (image) {
    URL.revokeObjectURL(image.previewUrl);
  }
  selectedImages = selectedImages.filter((item) => item.id !== id);
  renderSelectedImages();
}

function renderSelectedImages() {
  if (!selectedImages.length) {
    selectedImagePreview.innerHTML = `<p class="empty">No reference images selected.</p>`;
    return;
  }
  selectedImagePreview.innerHTML = "";
  for (const image of selectedImages) {
    const item = document.createElement("div");
    item.className = "selected-image-item";

    const img = document.createElement("img");
    img.src = image.previewUrl;
    img.alt = image.file.name;

    const removeButton = document.createElement("button");
    removeButton.type = "button";
    removeButton.className = "remove-image-btn";
    removeButton.textContent = "×";
    removeButton.setAttribute("aria-label", `Remove ${image.file.name}`);
    removeButton.addEventListener("click", () => removeSelectedImage(image.id));

    item.append(img, removeButton);
    selectedImagePreview.appendChild(item);
  }
}

async function readFileAsPayload(file) {
  const dataUrl = await new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(String(reader.result ?? ""));
    reader.onerror = () => reject(reader.error ?? new Error(`Failed to read ${file.name}`));
    reader.readAsDataURL(file);
  });
  const match = /^data:([^;,]+);base64,(.*)$/.exec(dataUrl);
  if (!match) {
    throw new Error(`Unsupported image encoding for ${file.name}`);
  }
  return {
    name: file.name,
    mimeType: match[1],
    dataBase64: match[2],
  };
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
imageInput.addEventListener("change", () => {
  addSelectedImages(Array.from(imageInput.files ?? []));
});
refreshBtn.addEventListener("click", loadProviders);
generateForm.addEventListener("submit", runGenerate);

renderSelectedImages();
loadProviders();
