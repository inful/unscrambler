(function () {
	function applyCorrectIndices(list, indices) {
		const indexSet = new Set(indices || []);
		Array.from(list.children).forEach((el, index) => {
			if (indexSet.has(index)) {
				el.classList.add("is-correct");
			} else {
				el.classList.remove("is-correct");
			}
		});
	}

	function enableSwapClick(list, onSwap) {
		let first = null;
		list.addEventListener("click", (event) => {
			const target = event.target.closest("li");
			if (!target || !list.contains(target)) return;
			if (!first) {
				first = target;
				first.classList.add("is-selected");
				return;
			}
			if (first === target) {
				first.classList.remove("is-selected");
				first = null;
				return;
			}
			const firstNext = first.nextSibling;
			const targetNext = target.nextSibling;
			list.insertBefore(first, targetNext);
			list.insertBefore(target, firstNext);
			first.classList.remove("is-selected");
			first = null;
			if (typeof onSwap === "function") {
				onSwap();
			}
		});
	}

	function initRound(container) {
		if (!container) return;
		const list = container.querySelector("[data-letters]");
		const progressUrl = container.dataset.progressUrl || "";
		const startMs = Number(container.dataset.startMs || "0");
		const durationSec = Number(container.dataset.durationSec || "0");
		const nextRoundMs = Number(container.dataset.nextRoundMs || "0");
		const isLocked = container.dataset.locked === "true";
		if (!list || !startMs || !durationSec || !progressUrl) return;

		const durationMs = durationSec * 1000;
		const halfTimeMs = startMs + durationMs / 2;
		let hasMarked = false;

		const guessString = () => {
			const letters = Array.from(list.children).map(
				(el) => el.dataset.letter || el.textContent || ""
			);
			return letters.join("");
		};

		const updateProgress = async () => {
			const body = new URLSearchParams({ guess: guessString() });
			try {
				const response = await fetch(progressUrl, {
					method: "POST",
					headers: { "Content-Type": "application/x-www-form-urlencoded" },
					body,
				});
				if (!response.ok) return;
				const payload = await response.json();
				if (Date.now() >= halfTimeMs) {
					applyCorrectIndices(list, payload.correctIndexes || []);
				}
			} catch (_err) {
				// Ignore transient network errors.
			}
		};

		if (!isLocked) {
			enableSwapClick(list, () => {
				updateProgress();
			});
		}

		const guessForm = container.querySelector("[data-guess-form]");
		const guessInput = container.querySelector("[data-guess-input]");
		if (guessForm && guessInput) {
			guessForm.addEventListener("submit", () => {
				guessInput.value = guessString();
			});
		}

		const timerEl = container.querySelector("[data-timer]");
		const nextTimerEl = container.querySelector("[data-next-timer]");
		const tick = () => {
			const now = Date.now();
			const remaining = startMs + durationMs - now;
			if (timerEl) {
				const seconds = Math.max(0, Math.ceil(remaining / 1000));
				timerEl.textContent = seconds + "s";
			}
			if (!hasMarked && now >= halfTimeMs) {
				updateProgress();
				hasMarked = true;
			}
			if (remaining <= 0) {
				container.classList.add("is-expired");
			}
			if (nextTimerEl && nextRoundMs) {
				const nextRemaining = nextRoundMs - now;
				const nextSeconds = Math.max(0, Math.ceil(nextRemaining / 1000));
				nextTimerEl.textContent = nextSeconds + "s";
			}
		};

		tick();
		const interval = setInterval(tick, 1000);
		container._roundInterval = interval;
	}

	function cleanupRound(container) {
		if (container && container._roundInterval) {
			clearInterval(container._roundInterval);
		}
	}

	function initAllRounds(root) {
		const scope = root || document;
		scope.querySelectorAll("[data-round]").forEach(initRound);
	}

	function replaceRoundArea(area, html) {
		if (!area) return;
		const currentRound = area.querySelector("[data-round]");
		const currentKey = currentRound ? currentRound.dataset.roundKey : "";
		const currentLocked = currentRound ? currentRound.dataset.locked === "true" : false;
		const tmp = document.createElement("div");
		tmp.innerHTML = html;
		const nextRound = tmp.querySelector("[data-round]");
		const nextKey = nextRound ? nextRound.dataset.roundKey : "";
		const nextLocked = nextRound ? nextRound.dataset.locked === "true" : false;
		if (currentKey && nextKey && currentKey === nextKey) {
			return;
		}
		if (currentKey && nextKey && !currentLocked && !nextLocked) {
			return;
		}
		area.querySelectorAll("[data-round]").forEach(cleanupRound);
		area.innerHTML = html;
		initAllRounds(area);
	}

	function initSSE() {
		const root = document.querySelector("[data-stream-url]");
		if (!root) return;
		const streamUrl = root.dataset.streamUrl;
		if (!streamUrl) return;
		const roundArea = document.getElementById("round-area");
		const playersArea = document.getElementById("players-area");
		const scoresArea = document.getElementById("scores-area");

		const source = new EventSource(streamUrl);
		source.addEventListener("round", (event) => {
			replaceRoundArea(roundArea, event.data);
		});
		source.addEventListener("players", (event) => {
			if (playersArea) {
				playersArea.innerHTML = event.data;
			}
		});
		source.addEventListener("scores", (event) => {
			if (scoresArea) {
				scoresArea.innerHTML = event.data;
			}
		});
	}

	document.addEventListener("DOMContentLoaded", () => {
		initAllRounds();
		initSSE();
	});

	document.addEventListener("click", async (event) => {
		const button = event.target.closest("[data-copy-url]");
		if (!button) return;
		const url = button.dataset.copyUrl || "";
		if (!url) return;
		try {
			await navigator.clipboard.writeText(url);
			button.textContent = "Copied!";
			setTimeout(() => {
				button.textContent = "Copy";
			}, 1200);
		} catch (_err) {
			// fallback: select the input for manual copy
			const input = button
				.closest(".field")
				?.querySelector("input[type='text']");
			if (input) {
				input.focus();
				input.select();
			}
		}
	});

	document.body.addEventListener("htmx:beforeSwap", (event) => {
		if (!event.target) return;
		if (event.detail && event.detail.shouldSwap === false) return;
		event.target.querySelectorAll("[data-round]").forEach(cleanupRound);
	});

	document.body.addEventListener("htmx:afterSwap", (event) => {
		if (!event.target) return;
		initAllRounds(event.target);
	});
})();
