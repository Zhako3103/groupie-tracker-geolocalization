const container = document.getElementById("tetris-container");
const searchInput = document.getElementById("search");
let allArtists = [];

// Загружаем всех артистов из Go API
async function fetchArtists() {
    const res = await fetch("/api/artists");
    allArtists = await res.json();
    startTetris(allArtists);
}

// Сколько артистов падает одновременно
const MAX_BLOCKS = 14;

function createFallingBlock(artist) {
    const block = document.createElement("a");
    block.className = "tetris-block";
    block.href = `/artist?id=${artist.id}`;

    const img = document.createElement("img");
    img.src = artist.image;
    const label = document.createElement("div");
    label.className = "block-label";
    label.textContent = artist.name;

    block.appendChild(img);
    block.appendChild(label);
    container.appendChild(block);

    const x = Math.random() * (window.innerWidth - 120);
    block.style.left = `${x}px`;

    const duration = 5 + Math.random() * 5;
    block.style.animationDuration = `${duration}s`;

    block.addEventListener("animationend", () => {
        block.remove();
        setTimeout(() => createFallingBlock(artist), Math.random() * 2000);
    });
}

function startTetris(list) {
    container.innerHTML = "";
    for (let i = 0; i < MAX_BLOCKS; i++) {
        const artist = list[Math.floor(Math.random() * list.length)];
        setTimeout(() => createFallingBlock(artist), i * 300);
    }
}

searchInput.addEventListener("input", () => {
    const query = searchInput.value.toLowerCase();
    const filtered = allArtists.filter(a => a.name.toLowerCase().includes(query));
    startTetris(filtered.length ? filtered : allArtists);
});

fetchArtists();